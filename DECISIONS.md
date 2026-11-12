# Kararlar

Her kayıt: ne seçildi, neden, neye karşı seçildi ve hangi koşulda yeniden düşünülür.
Kayıtlar proje ilerledikçe eklenir; eski kararlar değişirse silinmez, güncellenir ve nedeni yazılır.

---

## 1. Backend dili: Go

**Karar.** Backend Go ile yazıldı.

**Neden.** Sistemin zor kısımları eşzamanlılık: kuyruktan okuyan worker'lar, kanal başına hız
limiti, retry zamanlaması ve hem LLM çıktısını hem gönderim durumunu tarayıcıya akıtan
uzun ömürlü SSE bağlantıları. Go'da bunlar goroutine, `context` ve standart kütüphaneyle,
ek bir çalışma zamanı (PHP-FPM + ayrı worker süreci, Swoole vb.) olmadan yazılıyor.

**Alternatif.** PHP (Laravel kuyrukları hazır gelir), ama akış ve worker için ek altyapı ister.

---

## 2. Kuyruk: ayrı bir broker değil, Postgres tablosu

**Karar.** Gönderilecek her mesaj Postgres'te bir satır. Worker'lar işi
`SELECT … FOR UPDATE SKIP LOCKED` ile alıyor. Kuyruk tablosu aynı zamanda mesajın
durum kaydı (queued → sending → sent / failed).

**Neden.**
- **Tek doğruluk kaynağı.** Redis veya RabbitMQ kullanılsaydı mesaj durumu Postgres'te,
  iş ise broker'da duracaktı. İkisine ayrı ayrı yazmak "kayıt var ama iş kuyrukta yok" ya da
  "iş gitti ama kaydı yok" durumlarını mümkün kılar; bunu çözmek için outbox tablosu ve
  uzlaştırma süreci gerekir. Postgres'te kuyruğa koymak ile kaydı oluşturmak aynı
  transaction'dır, bu sınıf hata hiç oluşmaz.
- **Tekrar gönderimi engelleme** zaten veritabanı kısıtıyla sağlanıyor
  (`UNIQUE (campaign_id, user_id, channel)`). Broker bunu çözmüyor; yine Postgres gerekiyor.
- **Gecikmeli retry** tek kolonla (`next_attempt_at`) ifade ediliyor. Redis'te bunun için
  sorted set ve onu kuyruğa taşıyan ayrı bir süreç gerekirdi.
- **Ölçek yeterli.** Hedef kanal başına saniyede 100 mesaj ve birkaç bin alıcı.
  `SKIP LOCKED` ile Postgres bunun çok üzerinde iş çıkarır.
- **Daha az parça.** `docker compose up` ile kalkan servis sayısı ve hata yüzeyi küçük kalır.

**Bedeli.** Çok yüksek hacimde (saniyede on binlerce mesaj) Postgres kuyruğu tablo şişmesi
ve vacuum yüküyle darboğaz olur. Worker'lar yatay ölçeklendiğinde hız limiti de süreç içinde
tutulamaz (bkz. 3).

**Ne zaman değişir.** İlk adım kuyruğu değil, **hız limitini Redis'e taşımak** olur (bkz. 3).
Hacim Postgres'in kaldıramayacağı seviyeye çıkarsa durum Postgres'te kalır, iş dağıtımı
outbox pattern ile bir broker'a (Redis Streams / Kafka) devredilir.

---

## 3. Hız limiti: şimdilik süreç içinde, yatay ölçeklemede Redis

**Karar.** Kanal başına "saniyede en fazla 100 mesaj" limiti worker süreci içinde, kanal başına
bir token bucket ile uygulanıyor. Worker tek kopya çalışıyor.

**Neden.** Tek süreçte limit tamamen yerel bir sayaçtır; doğru, test edilebilir ve ek servis
gerektirmez.

**Ne zaman değişir.** Worker birden fazla kopya halinde çalıştırılırsa her kopyanın ayrı
sayacı toplam limiti aşar (3 kopya × 100/sn = 300/sn). O noktada limit, tüm kopyaların
paylaştığı bir Redis token bucket'ına (atomik Lua script veya `INCR` + `EXPIRE` ile
saniyelik pencere) taşınır. Bu, Redis'in bu projeye ilk gireceği yerdir; kuyruk Postgres'te
kalabilir.

---

## 4. Davranış verisi: özet kolonlar değil, olay tablosu

**Karar.** Sepete ekleme ve satın almalar `events` tablosunda tek tek tutuluyor
(`user_id, event_type, category, amount, occurred_at`). Kullanıcı tablosunda
`last_purchase_at` gibi özet kolonlar yok.

**Neden.** Örnek brief zaman pencereli ve birbirine bağlı iki koşul içeriyor: "son 30 günde
sepete ekledi **ve** son 30 günde satın almadı". Özet kolonlar yalnızca önceden düşünülmüş
soruları cevaplar; "son 7 günde kozmetik sepete ekleyen" gibi yeni bir brief şema değişikliği
gerektirirdi. Olay tablosu üzerinde segment derleyicisi her koşulu parametreli bir
`EXISTS` / `NOT EXISTS` alt sorgusuna çeviriyor; `(user_id, event_type, occurred_at)`
indeksi bu sorguları karşılıyor.

**Bedeli.** Sorgular özet kolona göre daha pahalı. Birkaç bin kullanıcı ve ~23 bin olayda
önemsiz; milyonlarca kullanıcıda olaylardan türetilmiş, periyodik güncellenen bir özellik
tablosu gerekir.

---

## 5. Seed verisi deterministik

**Karar.** Seed, `(kullanıcı sayısı, rastgele seed, referans zaman)` girdilerinin saf bir
fonksiyonu. Varsayılan 5000 kullanıcı, seed 42. Seed idempotent: kullanıcı varsa atlanır.

**Neden.** Aynı ayarlar her makinede aynı kitleyi üretir, böylece testler ve demo
"İstanbul'da sepette bırakan" kitlenin büyüklüğüne güvenebilir. `docker compose up` tekrar
çalıştırıldığında veri çoğalmaz.

**Dağılım.** Şehirler nüfusa benzer ağırlıklarla (%30 İstanbul), %10 İngilizce konuşan kullanıcı
(dile göre içerik bonusu için), kullanıcıların %25'i son 120 günde hiç olay üretmemiş,
sepetlerin ~%35'i üç gün içinde satın almaya dönüşüyor. Örnek brief bu veride 381 kişilik bir
kitle verir; bunların 222'sine SMS, 254'üne email gönderilebilir, 58'ine hiçbir kanaldan
ulaşılamaz.

---

## 6. Kanal izni ve adres birlikte zorunlu

**Karar.** Bir kullanıcıya bir kanaldan mesaj ancak o kanala izin vermişse **ve** o kanal
için adresi varsa gönderilir. Seed'de adresi olmayan kullanıcı o kanala izin veremez;
gönderim tarafı da iki koşulu ayrıca kontrol eder.

**Neden.** Case "izni olmayan kullanıcıya o kanaldan mesaj gönderilmez" diyor. Adressiz ama
izinli bir kayıt, kitle sayısını şişirir ve gönderimde kesin hataya dönüşür.

---

## 7. Segment kuralı yalnızca kapalı değer kümeleriyle çalışır

**Karar.** Şehir ve ürün kategorisi gibi alanlar serbest metin değil, verideki kapalı
kümelerden (ör. 15 şehir, 6 kategori) biri olmak zorunda. Kural doğrulayıcısı kümede olmayan
değeri reddeder.

**Neden.** LLM "Istanbul", "İst." ya da veride olmayan bir şehir üretebilir. Kapalı küme,
bu durumu sessizce sıfır kişilik bir kitleye dönüştürmek yerine açık bir doğrulama hatasına
çevirir; bu hata Claude'a geri verilip düzeltmesi istenebilir.

---

## 8. Migration'lar binary'ye gömülü, advisory lock ile korunuyor

**Karar.** SQL migration'ları `embed` ile binary'nin içinde. Hem `init` (seed) hem `api`
başlangıçta migration çalıştırıyor; `pg_advisory_lock` aynı anda iki sürecin uygulamasını
engelliyor. Her migration kendi transaction'ında.

**Neden.** Harici bir migration aracı ve ek container gerekmiyor; imaj ile şema sürümü her zaman
uyumlu.

---

## 9. Sağlık kontrolü: liveness ve readiness ayrı

**Karar.** `GET /healthz` yalnızca sürecin ayakta olduğunu söyler, bağımlılıklara bakmaz.
`GET /readyz` veritabanına ping atar ve ulaşılamıyorsa 503 döner.

**Neden.** Veritabanı yavaşladığında orkestratörün sağlıklı API süreçlerini yeniden
başlatmasını istemeyiz (liveness), ama trafiğin kesilmesini isteriz (readiness).

---

## 10. LLM modu: key yoksa mock'a düş

**Karar.** `LLM_MODE=auto` (varsayılan) iken `ANTHROPIC_API_KEY` varsa Claude, yoksa mock LLM
kullanılır. `mock` ve `anthropic` modları zorlanabilir; `anthropic` modu key yoksa açılışta
hata verir.

**Neden.** Değerlendiren kişinin key'i olmayabilir; `docker compose up` yine de uçtan uca
çalışan bir demo vermeli. Testler her zaman mock ile çalışır.

---

## 11. Test stratejisi

**Karar.** Birim testleri bağımlılıksız çalışır. Veritabanı gerektiren testler her test için
ayrı bir Postgres şeması oluşturur ve sonunda siler; `TEST_DATABASE_URL` yoksa atlanır.
`make test` her şeyi container içinde, geçici (tmpfs) bir Postgres'e karşı çalıştırır.

**Neden.** Değerlendirenin makinesinde Go, Postgres veya API key gerekmez. Ayrı şemalar
testlerin birbirinin verisini görmesini engeller ve paralel çalışmalarına izin verir.

---

## 12. Veritabanı kısıtları: yapısal kurallar CHECK ile, iş değerleri uygulamada

**Karar.** `002_events_amount_check` migration'ı "satın almanın tutarı vardır, sepete eklemenin
yoktur" kuralını bir `CHECK` kısıtı yaptı. Kategori (`events.category`) ve şehir (`users.city`)
için `CHECK` konmadı; kapalı kümelerin tek kaynağı Go'daki `seed.Categories` ve `seed.Cities()`.

**Neden.**
- Tutar kuralı yapısal: olay tipinin anlamından gelir, iş değiştikçe değişmez. Bir kez yazılır,
  bakım istemez.
- Kategori listesi iş verisi. Gerçek bir sistemde ürün kataloğundan gelir ve değişir. Listeyi
  SQL'e de yazmak ikinci bir doğruluk kaynağı yaratır: her yeni kategori hem Go'da hem bir
  migration'da değişmeli, biri unutulursa ikisi ayrışır.
- Kapalı kümenin koruduğu risk LLM'in ürettiği segment kuralı (bkz. 7). LLM çıktısı `events`
  tablosuna yazılmaz, yalnızca onu sorgulayan kurala dönüşür; koruma doğru yerde, doğrulayıcıda.
  `events`'e bugün yalnızca seed yazıyor ve `TestGenerateInvariants` kategorileri kontrol ediyor.

**Alternatif.** `CHECK (category IN (...))` ya da bir `categories` tablosu ve foreign key.
Foreign key tek kaynak sorununu çözer, ama doğrulayıcının listeyi veritabanından okumasını
gerektirir; doğrulayıcının birim testleri veritabanına bağımlı hale gelir.

**Ne zaman değişir.** `events`'e seed dışında bir yazan eklenirse (ör. gerçek olay toplayan bir
API), kategoriler bir `categories` tablosuna taşınır, foreign key konur ve doğrulayıcı listeyi
açılışta oradan yükler.

---

## 13. Segment kuralı formatı: düz, tipli koşul listesi

**Karar.** Kural `match` (`all` / `any`) ve en fazla 10 koşuldan oluşan düz bir liste. Koşul iki
tipten biri: `attribute` (`city`, `language`, `gender` için `in` / `not_in`; `age` için
`between` + `min` / `max`) ya da `event` (`cart_add` / `purchase`, opsiyonel `category`,
`within_days` 1–365, `exists`). İç içe grup yok. Kuralda serbest metin alan yok.

```json
{
  "match": "all",
  "conditions": [
    { "type": "attribute", "field": "city", "op": "in", "values": ["İstanbul"] },
    { "type": "event", "event": "cart_add", "within_days": 30, "exists": true },
    { "type": "event", "event": "purchase", "within_days": 30, "exists": false }
  ]
}
```

**Neden.**
- **Şemaya uyan her kural anlamlı.** Her koşul tipinin alanları sabit; doğrulayıcı tüm
  ihtimalleri kapsayabiliyor. Başka tipe ait bir alan (`city` koşulunda `within_days`) ya da
  bilinmeyen bir alan hatadır.
- **Claude tarafında da zorlanabiliyor.** Claude'un structured outputs özelliği özyinelemeli
  şemayı desteklemiyor. Düz format `anyOf` ile tam ifade ediliyor, kapalı kümeler (`enum`) dahil;
  şema doğrulayıcının kullandığı tablolardan üretiliyor (`segment.JSONSchema`), ikisi ayrışamaz.
- **Derleyici basit.** Her koşul sabit bir SQL kalıbına ve bind parametrelerine dönüşüyor;
  parantezleme ya da özyineleme yok.

**Şemanın yetmediği yer.** Structured outputs sayısal sınır (`within_days` 1–365), dizi uzunluğu ve
koşullar arası kuralları ifade edemiyor; mock LLM zaten şemaya bağlı değil. Bu yüzden asıl otorite
backend: `segment.Parse` ayrıca aralıkları, tekrar eden alanı ve birlikte kimseyi seçmeyen
koşulları ("son 7 günde aldı" + "son 30 günde almadı") reddediyor. `segment.Compile` kendi
başına da doğruluyor; Go'da elle kurulmuş bir kural da doğrulanmadan SQL'e ulaşamıyor.

**Yaş.** Taslaktaki `birth_year` yerine `age` kullanıldı. Pazarlamacı "25–34 yaş" der; yılı
Claude'a hesaplatmak gereksiz bir hata kaynağı. Backend `asOf` yılından doğum yılı aralığını
kendisi çıkarıyor (yaklaşık: ay ve gün yok).

**Alternatif.** İç içe `all` / `any` / `not` ağacı ve genel `field` / `op` / `value` yaprakları
(JSON Logic benzeri). Her kombinasyonu ifade eder; ama şema özyinelemeli olur (Claude tarafında
zorlanamaz), "şemaya uyan ama anlamsız" kurallar mümkün olur (`city` + `occurred_within_days`,
altı seviye `not`), aynı niyet birden çok şekilde yazılabilir ve derinlik sınırı ayrıca gerekir.

**Bedeli.** "(İstanbul ve sepet) veya (Ankara ve satın alma)" gibi iç içe mantık ifade
edilemiyor. Bu durumda Claude "desteklenmiyor" der (bkz. 15) ve pazarlamacı brief'i iki
kampanyaya böler.

**Ne zaman değişir.** İç içe mantık gerçekten gerekirse tek seviye grup eklenir (koşul listesi
yerine grup listesi); bu hâlâ özyinelemesiz bir şemadır.

---

## 14. Kitle açıklamasını backend üretir, Claude yalnızca yorumlarını bildirir

**Karar.** Pazarlamacıya gösterilen kitle açıklaması Claude'dan gelmez; `segment.Describe`
onu kuraldan türetir ("Şehir: İstanbul", "Son 30 günde hiç satın alma yapmadı"). Claude ayrıca
`assumptions` listesi döner: brief'teki belirsiz bir ifadeyi nasıl yorumladığını, ilgili
koşulun indeksiyle birlikte yazar. Arayüz bunu o koşulun satırının yanında gösterir.

**Neden.**
- İnsan onayı güvenlik mekanizmasıdır; insanın okuduğu şey SQL'e derlenen şeyle aynı kaynaktan
  gelmeli. Claude'un yazdığı açıklama kuralla çelişirse ("son 30 gün" yazar, kural 7 gündür)
  insan yanlış şeyi onaylar ve bunu hiçbir doğrulayıcı yakalayamaz, çünkü serbest metin
  doğrulanamaz.
- Yorumlar ise gizlenmemeli. "Alışveriş yapmayan" iki şekilde okunabilir: son 30 günde hiç
  satın almamış, ya da sepete eklediği ürünü almamış. Format yalnızca ilkini ifade edebiliyor;
  pazarlamacı bunu bilerek onaylamalı.

**Sınırı.** `assumptions` de serbest metin, doğruluğu kontrol edilemez. Bu yüzden açıklamanın
yerine değil yanına konur; backend yalnızca indeksin gerçek bir koşulu gösterdiğini ve
uzunluğu kontrol eder. Çelişki olursa doğru olan türetilmiş açıklamadır.

**Alternatif.** Kuralın içinde Claude'un yazdığı bir `explanation` alanı (ilk taslak). Daha
doğal bir cümle verir, ama yukarıdaki çelişki riskini taşır.

---

## 15. Claude "desteklenmiyor" diyebilir; kısmi kural üretilmez

**Karar.** Üretim çıktısı iki tipten biridir: `kind: "rule"` (kural + yorumlar) ya da
`kind: "unsupported"` (neden + ifade edilemeyen ifadeler). Brief'in herhangi bir kitle koşulu
formatla tam ifade edilemiyorsa Claude kural üretmez. Endpoint bu durumda 200 ve
`status: "unsupported"` döner; sorgu çalışmaz.

**Neden.** Çıkış yolu olmayan bir şema modeli uydurmaya zorlar: "500 TL üzeri harcayan
İstanbullular" için en yakın geçerli çıktı "İstanbullular"dır, yani istenenden geniş bir kitle.
Yanlış kitleye gönderim, gönderememekten daha pahalı bir hatadır.

**Alternatif.** Desteklenen kısım için kural + desteklenmeyen kısımların listesi (kısmi kural).
Pazarlamacıya bir başlangıç verir; ama eksik koşullu kural her zaman daha geniş bir kitledir ve
uyarı gözden kaçarsa onaylanabilir. Pazarlamacı brief'i sadeleştirip yeniden deneyebilir.

**Not.** Ton, indirim oranı gibi içerik talimatları kitle koşulu değildir; prompt bunların
"desteklenmiyor" nedeni olmadığını açıkça söyler.

---

## 16. Claude istemcisi: resmi Go SDK, `llm.Client` arayüzünün arkasında

**Karar.** Claude'a `anthropic-sdk-go/v2` ile gidilir. SDK'yı yalnızca `internal/llm/anthropicllm`
import eder; uygulamanın geri kalanı `llm.Client` / `llm.Streamer` arayüzünü görür. Her istek
akışlı (streaming) gönderilir, şema `output_config.format` (structured outputs) ile verilir.
Model `ANTHROPIC_MODEL` ile değişir, varsayılan `claude-opus-5-5`.

**Neden.**
- Aşama 3 akışlı üretim gerektiriyor. SSE ayrıştırıcısını, 429 / 5xx retry'ını ve hata
  eşlemesini elle yazmak puan getirmeyen bir hata yüzeyi; SDK bunları hazır veriyor ve API
  değişikliklerini takip ediyor.
- İstemci arayüzün arkasında kaldığı için testler etkilenmiyor: uygulama testleri `llm.Scripted`
  ve mock ile, istemcinin kendi testleri Messages API'nin SSE formatını konuşan yerel bir
  `httptest` sunucusuyla çalışıyor. Hiçbiri API key ya da ağ istemiyor.

**İstemcinin sorumluluğu.** Yalnızca taşıma. Şemaya uyan yanıt geçerli yanıt değildir; ayrıştırma,
doğrulama ve yeniden deneme çağıranda (`segment.Generator`) kalır. İstemcinin kendi ele aldığı
durumlar: `refusal` (`ErrRefused`), `max_tokens` (`ErrTruncated`, yarım JSON döndürülmez), metinsiz
yanıt (`ErrEmpty`). Reddedilen bir yanıt yeniden denemede modele üretildiği haliyle (thinking
blokları dahil) geri gönderilir; yalnızca metni geri vermek modelin akıl yürütmesini kaybettirir.

**Bedeli.**
- Yeni bağımlılık (ve dolaylı olarak OpenTelemetry API paketleri).
- SDK v2 Go 1.25 istiyor; derleme imajı ve `go.mod` 1.24'ten 1.25'e çıktı.

**Bilinçli olarak yapılmayan.** Sunucu taraflı refusal fallback (beta) açık değil: bir pazarlama
brief'inde ret beklenmiyor ve beta başlığına bağımlılık istemedim. Ret olursa açık bir hata döner.
Thinking ve effort modelin varsayılanında bırakıldı; böylece aynı istek her güncel modelde çalışır.

**Alternatif.** Ham `net/http`: sıfır bağımlılık, her satır bizim; ama SSE, retry ve hata
eşlemesini kendimiz yazıp test ederiz.

---

## 17. İçerik tek dilde; dile göre içerik bonusu yapılmadı

**Karar.** Kampanya başına tek bir içerik seti üretilir (`sms`, `email.subject`, `email.body`).
Seed'deki `language = en` kullanıcılar da aynı metni alır. Bonus README'de "yapılmadı" olarak
durur; aşama 5'ten sonra zaman kalırsa bakılır.

**Neden.** Dil başına varyant her aşamaya iş ekler: üretim süresi ve çıktı iki katına çıkar,
pazarlamacı iki metni onaylar, test matrisi büyür, gönderici şablon seçmek zorunda kalır. Bonus
puanı çekirdek akışın dört aşamasına (15 Kasım sınırı) değmez.

**Sonradan eklemeyi ucuz tutan şey.** `content.Content.Validate` tek bir içerik seti üzerinde
çalışır; dil eklenirse şema bu setlerin listesi olur, doğrulayıcı yeniden yazılmaz.

**Bilinçli olarak yapılmayan.** Aşama 4'teki `messages` tablosuna şimdiden `language` kolonu
eklenmeyecek. Kullanılmayan kolon test edilemez; mesajın dili `users.language`'dan zaten
türetilebilir; kolonu sonradan eklemek tek satırlık bir migration. Pahalı olan migration değil,
şablon seçimi ve onay ekranı.

**Alternatif.** Şemayı baştan `variants: [{language, sms, email}]` olarak kurmak. Bonus
karşılanır ve aşama 4–5 tek seferde doğru şekille kurulur; bedeli yukarıdaki ek iş.

**Güncelleme (12 Kasım).** Çekirdek akış ve teslim aşaması bittikten sonra bonus yapıldı; bu
karar 30 ile değişti. Burada öngörülen iki şey tuttu: doğrulayıcı yeniden yazılmadı (her dil
aynı `Validate`'ten geçiyor) ve `messages` tablosuna dil kolonu yine eklenmedi.

---

## 18. İçerik akışı: backend alan bazlı olaylar yollar

**Karar.** `POST /api/content/generate` bir SSE akışı döner. Claude'un çıktısı JSON'dur; backend
bu JSON'u akarken çözer (`content.fieldStream`) ve alan alan olay yollar:

| Olay | Anlamı |
|---|---|
| `attempt` | Yeni bir deneme başladı; ekrandaki metin temizlenir |
| `delta` | Bir alanın (`sms`, `email.subject`, `email.body`) devamı; henüz doğrulanmadı |
| `rejected` | Denemenin çıktısı doğrulamadan geçemedi (hata listesiyle) |
| `result` | Doğrulanmış içerik, ölçülen uzunluklar, limitler; akış biter |
| `error` | Üretim başarısız (`llm_output_invalid` ya da `llm_unavailable`); akış biter |

**Neden.**
- Arayüz yarım JSON ayrıştırmaz. Kaçış karakterlerini (`\n`, `\"`, `\uXXXX`, surrogate çiftleri)
  ve parçalar arasında bölünen karakterleri çözmek tek yerde, Go'da yazılıp test edilir.
- Yeniden deneme görünür olur: pazarlamacı neden beklediğini ve neyin reddedildiğini görür.
- Akan metin önizlemedir, sonuç değildir. Yalnızca `result` olayı doğrulanmış içerik taşır;
  reddedilen bir metin hiçbir zaman `result` olarak gitmez.

**Sınırı.** Çözücü geçerlilik kararı vermez; bozuk girdide çıkarabildiğini yollar, kararı
`content.Parse` verir. Doğruluğu `encoding/json` ile karşılaştırılarak test edilir (her parça
boyutu için).

**Alternatif.** Modelin ham metnini iletmek. Backend'de en az kod; ama aynı çözücü
TypeScript'te yazılır ya da ekranda ham JSON akar.

---

## 19. Karakter limiti: en kötü durum, tüm kullanıcıların en uzun değeriyle

**Karar.** SMS (160) ve email konusu (80) limiti, şablondaki her değişken alabileceği en uzun
değerle doldurulmuş gibi ölçülür. En uzun değer `users` tablosunun tamamından alınır
(`content.DBLengths`, bir dakika önbellekte), kampanyanın kitlesinden değil. Sayım rune ile.
İzinli değişkenler kapalı küme: `{{first_name}}`, `{{last_name}}`, `{{city}}`.

**Neden.**
- Hangi kitle seçilirse seçilsin güvenli üst sınır. İçerik üretimi segment kuralından bağımsız
  kalır; pazarlamacı kuralı sonradan düzenlerse içerik geçersizleşmez.
- Kitleye göre ölçmek birkaç karakter kazandırır, ama onay ile gönderim arasında kitleye giren
  uzun isimli bir kullanıcı limiti aşabilir ve gönderimde ayrıca kontrol gerekir.
- Limit aşımı hatası ölçülen sayıyı ve ne kadar kısaltılacağını söyler ("165 karakter, limit
  160; en az 5 karakter kısaltın; {{first_name}} 10 karakter sayılır"). Aynı metin Claude'a
  geri verilir; model tahmin etmek yerine hedefe göre kısaltır.

**Bedeli.** Kitlede o kadar uzun değer yoksa birkaç karakter kullanılmadan kalır.

**Bilinen sadeleştirme.** Limit case'teki gibi karakter sayısıdır. Gerçek SMS'te Türkçe
karakterler mesajı UCS-2'ye çevirir ve tek parça sınırı 70 karaktere düşer; bu hesap yapılmıyor.
Gerçek bir provider'a geçerken `content.checkField` içine eklenir.

**Alternatif.** Seçilen kitlenin en uzun değeri (yukarıda).

---

## 20. Segment kuralının kapsamı şimdilik genişletilmiyor; kısmi kural yine yok

**Bağlam.** Örnek brief tek bir durum. Gerçek bir pazarlamacı üyelik tarihi, sadakat seviyesi,
cihaz, ürün görüntüleme, "en az 3 kez", toplam harcama, sipariş sayısı ve iç içe VE / VEYA da
isteyebilir. Aşama 4'ten önce bunların ne kadarının karşılandığı gözden geçirildi.

**Karar.**
- Kural formatı aşama 2'deki haliyle kalır (DECISIONS 13). Karşılanmayan her istek
  `unsupported` yanıtına düşer (DECISIONS 15); yanlış ya da eksik koşullu bir kitle üretilmez.
- "Yapabildiği kısım için kural + uyarı listesi" (kısmi kural) yine yapılmıyor: 15. kararı
  yeniden değerlendirdik ve koruduk.
- Aşama 5'ten sonra zaman kalırsa, verisi hazır olan üç şey eklenir: olay sayısı (`min_count`),
  harcama / sipariş metrikleri (satın alma olaylarının `amount` alanından), üyelik tarihi
  (`signup_at`). **Güncelleme:** üçü de eklendi, bkz. 25. karar. Aşağıdaki tablo o tarihteki
  durumu gösterir.

| İstek | Durum |
|---|---|
| Şehir, dil, cinsiyet, yaş aralığı | Var |
| Sepete ekleme / satın alma, yaptı / yapmadı, kategori, son N gün | Var |
| "60 gündür alışveriş yapmayan" | Var (satın alma, 60 gün, yapmadı) |
| VE / VEYA, tek seviye | Var |
| Üyelik tarihi, olay sayısı, toplam harcama, sipariş sayısı | Yok; veri var, format yok |
| İç içe gruplar | Yok; bilinçli (DECISIONS 13) |
| Ülke, sadakat seviyesi, cihaz; ürün görüntüleme, uygulama / email açma | Yok; seed'de veri yok |

**Neden.**
- Aşama 4 ve 5, 15 Kasım sınırına bağlı; case "çekirdek akışı sağlam yap" diyor.
- Verisi olmayan alanlar seed'i değiştirmeyi gerektirir. Seed deterministik olduğu için bu,
  381 / 222 / 254 / 58 referans sayılarını ve onlara bağlı testleri kaydırır.
- Kapsam dar olsa da hata modu güvenli: desteklenmeyen istek görünür bir "desteklenmiyor"
  yanıtıdır, sessizce genişlemiş bir kitle değil.

**Bilinen sınır.** Claude bir koşulu hem atlar hem `unsupported` demezse bunu hiçbir doğrulayıcı
yakalayamaz. Savunma, prompt'taki açık yasak ve pazarlamacının kuraldan türetilen açıklamada
(DECISIONS 14) eksik koşulu görebilmesi.

**Alternatif.** Kısmi kural: `rule` + `unsupported: [...]` birlikte döner, arayüz uyarı gösterir,
karar pazarlamacıya kalır. Pazarlamacıya bir başlangıç verir; ama eksik koşullu kural her zaman
daha geniş kitledir ve uyarı gözden kaçarsa onaylanır.

---

## 21. Retry durumu veritabanında; üstel backoff, 429'da kanal duraklar

**Karar.** Her mesajın deneme sayısı ve bir sonraki deneme zamanı kendi satırında durur
(`attempts`, `next_attempt_at`). Worker zamanı gelen satırları `FOR UPDATE SKIP LOCKED` ile
kiralar (`status = 'sending'`, `locked_until`); HTTP çağrısı sırasında transaction açık tutulmaz.

| Sonuç | Davranış |
|---|---|
| 2xx | `sent` |
| Ağ hatası, zaman aşımı, 5xx | Üstel backoff + jitter (1s, 2s, 4s, 8s; adımın yarısı ile tamamı arası) ile yeniden denenir; toplam 5 gönderimden sonra `failed` |
| 429 | Kanalın hız sınırlayıcısı `Retry-After` kadar durur (en fazla 1 dk; başlık yoksa 2 sn); mesaj deneme hakkı harcamaz |
| Diğer 4xx | Hemen `failed`; aynı istek yine reddedilir |
| Onaydan sonra izin ya da adres kalktıysa | `skipped`; provider çağrılmaz |

Her denemede aynı idempotency key gider (`campaign-<id>-user-<id>-<kanal>`).

**Neden.**
- **Çökmeye dayanıklı.** Süreç ölürse bekleyen retry kaybolmaz; kirası dolan satırı başka bir
  worker alır. Deneme sayısı sıfırlanmaz.
- **Bekleyen mesaj worker'ı meşgul etmez.** Backoff'taki mesaj bir satırdır, uyuyan bir
  goroutine değil.
- **Zaman aşımı "gitmedi" demek değildir.** Provider mesajı kabul edip yanıtı kaybedebilir (mock
  provider bunu bilerek yapar). Bu yüzden aynı kişiye iki kez gitmeme garantisi iki katmanlı:
  veritabanında `UNIQUE (campaign_id, user_id, channel)` bir kişi için tek satır olmasını,
  idempotency key o satırın kaç kez denenirse denensin bir kez teslim edilmesini sağlar.
- **429 mesajın değil kanalın sorunudur.** Tek mesajı bekletip diğerlerini göndermeye devam
  etmek provider'ı daha da zorlar; bu yüzden kanalın tamamı durur.
- **Eski sonuç yeni durumu ezmez.** Sonuç yazan sorgu `status = 'sending' AND attempts = N`
  koşulu taşır; kirası dolup başkasına geçmiş bir satırın gecikmiş sonucu yok sayılır.

**Bedeli / sınırı.**
- Worker API süreci içinde çalışır (`WORKER_ENABLED`). Hız sınırlayıcı süreç içinde olduğu için
  (DECISIONS 3) gönderen süreç tek olmalı; ikinci bir API kopyası `WORKER_ENABLED=false` ile
  açılır. Kuyruk tarafı (`SKIP LOCKED`) birden çok worker'a hazır ve testte iki worker ile
  doğrulanıyor.
- Provider sürekli 429 dönerse mesaj süresiz `pending` kalır; bir üst sınır (ör. toplam bekleme
  süresi) yok.
- 429 geldiği anda yolda olan istekler (kanal başına en fazla 32) geri çağrılamaz.

**Alternatif.** Retry'ı worker içinde tutmak (mesajı elde bekletip yeniden denemek). Daha az kod
ve daha basit şema; ama bekleme sırasında worker bloklu kalır ve çökmede deneme sayısı kaybolur.

---

## 22. Şablonlar gönderim anında doldurulur; alıcılar onayda dondurulur

**Karar.** Onayda (`POST /api/campaigns`) kampanya ve kitlesi tek transaction'da yazılır: kural
ve içerik `campaigns` satırına, alıcılar kanal başına tek `INSERT … SELECT` ile `messages`
tablosuna. Metin o anda üretilmez; worker mesajı gönderirken kullanıcıyı okur, şablonu doldurur
ve izni yeniden kontrol eder.

**Neden.**
- **Kuyruğa alma atomik ve tek sorgu.** Ya kitlenin tamamı kuyruğa girer ya hiçbiri. Kitle
  koşulu, sayımı yapan derleyiciden ve aynı `SMSReachable` / `EmailReachable` tanımlarından
  gelir; pazarlamacının gördüğü sayı ile kuyruğa giren sayı aynıdır (testte 222 / 254).
- **İzin gönderim anında geçerli olmalı.** Onay ile gönderim arasında izni ya da adresi kalkan
  kişi atlanır (`skipped`).
- **Limit yine korunur.** Doldurulmuş metin gönderimden önce bir kez daha ölçülür; onaydan sonra
  eklenen daha uzun bir isim 160 / 80 sınırını aşarsa mesaj gönderilmez, `failed` olur.

**Bedeli.** Gönderilen metnin kendisi saklanmaz; şablon ve kullanıcıdan yeniden üretilebilir, ama
kullanıcı sonradan adını değiştirirse birebir aynısı olmaz. Denetim kaydı gerekirse `messages`'a
bir `rendered_body` kolonu eklenir.

**Alternatif.** Kuyruğa alırken doldurmak: her alıcının metni onayda üretilip satıra yazılır.
Gönderilen metin denetlenebilir ve worker çok basit olur; ama kuyruğa alma Go'da döngü + toplu
ekleme gerektirir ve izin değişikliği için yine gönderimde ayrı kontrol gerekir.

---

## 23. Canlı pano SSE ile; metrics Prometheus metin formatında

**Karar.**
- `GET /api/campaigns/{id}/events` bir SSE akışıdır: bağlanınca `status` olayı gelir, sonra
  yalnızca sayılar değiştiğinde yeni bir `status`; bekleyen mesaj kalmayınca `done` gelir ve
  akış kapanır. Backend durumu saniyede bir yeniden okur.
- `GET /metrics` Prometheus metin formatındadır: `scc_campaigns`, `scc_messages{channel,status}`
  (her istekte veritabanından okunan gauge'lar) ve `scc_provider_requests_total{channel,outcome}`
  (sürecin sayacı; `ok`, `error`, `rate_limited`, `rejected`).

**Neden.**
- İçerik üretimiyle aynı mekanizma: frontend'de tek desen var ve case'in "canlı gönderim durumu"
  gereksinimi doğrudan karşılanıyor. Bağlantı koparsa tarayıcının `EventSource`'u kendisi yeniden
  bağlanır ve ilk olay güncel durumdur; kaçan olay diye bir şey yok.
- Pano, `GET /api/campaigns/{id}` ve `/metrics` aynı satırları sayar (`campaign.Store.Status` /
  `Totals`); sayıların birbirini tutmaması mümkün değil.
- Metrics için istemci kütüphanesi eklenmedi: birkaç satırlık metin formatı bir bağımlılığı haklı
  çıkarmıyor (`internal/metrics`).

**Bedeli / sınırı.**
- Açık her pano bağlantısı saniyede bir sorgu demek. Çok sayıda izleyici olursa durum, worker'ın
  yazdığı yerde yayınlanır (Postgres `LISTEN / NOTIFY`) ve sorgu ortadan kalkar.
- `scc_provider_requests_total` süreç içi bir sayaçtır; süreç yeniden başlayınca sıfırlanır
  (Prometheus sayaçları için beklenen davranış). Kuyruk sayıları veritabanından geldiği için
  yeniden başlamadan etkilenmez.
- LLM çağrıları için metrik yok (deneme sayısı, reddedilen çıktı sayısı); yalnızca loglanıyor.
- Metrics formatını ben seçtim (JSON yerine Prometheus metni); ayrı bir seçenek turu yapılmadı.

**Alternatif.** Frontend'in saniyede bir `GET /api/campaigns/{id}` çağırması. Backend'de yeni kod
gerekmez; ama iki farklı veri alma deseni olur ve "canlı endpoint" gereksinimi zayıf karşılanır.

---

## 24. Arayüz ayrı bir container'da, nginx arkasında

**Karar.** Vue uygulaması `frontend/` altında kendi Dockerfile'ı ile derlenir; nginx statik
dosyaları sunar ve `/api` isteklerini API servisine iletir. Tarayıcı tek bir origin görür
(`http://localhost:3000`). `/api` için tamponlama kapalıdır (`proxy_buffering off`), yoksa SSE
olayları akış bitince toplu gelir.

**Neden.**
- Backend ve frontend bağımsız derlenir; Go binary'si saf API kalır ve derlemesi Node'a
  bağlanmaz.
- Aynı origin olduğu için CORS ayarı gerekmez.

**Arayüzdeki doğrulama bir kolaylıktır, yetki değildir.** Canlı karakter sayacı için şablon
ölçümü TypeScript'te de yazıldı (`frontend/src/lib/template.ts`); backend'deki
`internal/content` ile aynı kuralları uygular ve aynı test durumlarıyla sınanır. Onay butonu
buna göre kapanır, ama `POST /api/campaigns` kuralı ve metinleri her zaman yeniden doğrular;
ikisi çelişirse backend kazanır. Değişkenlerin uzunlukları arayüze backend'den gelir.

**Bedeli.** Bir servis ve bir nginx ayarı daha; ölçüm kuralları iki dilde duruyor ve biri
değişirse diğeri de güncellenmeli (ortak test durumları bunu yakalamak için var).

**Alternatif.** Derlenmiş arayüzü Go binary'sine gömmek. Tek servis olur ve arada proxy kalmaz;
ama Go imajının derlemesi Node'a bağlanır ve frontend değişince backend imajı yeniden derlenir.

---

## 25. Segment kuralına olay sayısı, üyelik tarihi ve satın alma metrikleri eklendi

**Bağlam.** 20. karar bu üçünü "zaman kalırsa" diye ertelemişti. Aşama 5 bittikten sonra farklı
brief'lerle denendi; "1000 TL üzeri harcayan", "en az 3 kez" ve "son 7 günde kayıt olan" gibi
olağan istekler "desteklenmiyor" yanıtı alıyordu. Üçünün de verisi seed'de hazırdı.

**Karar.** Düz liste formatı (13. karar) korunarak üç ekleme yapıldı:

```json
{ "type": "event", "event": "cart_add", "category": null, "within_days": 30, "exists": true, "min_count": 3 }
{ "type": "signup", "op": "within", "days": 7 }
{ "type": "metric", "metric": "total_spent", "op": "gte", "value": 1000, "within_days": null }
```

- **`min_count`** (olay koşulunda, opsiyonel): olay pencerede en az N kez oldu (1–100). Yalnızca
  `exists: true` ile.
- **`signup`**: `within` (son N günde üye oldu) ya da `older_than` (N günden önce üye oldu).
  İkisi birlikte bir aralık verir ("30–90 gün önce üye olanlar").
- **`metric`**: `total_spent` (TL) ya da `order_count`, `gte` (en az) / `lte` (en fazla), tüm
  zamanlar ya da son N gün. `gte` ve `lte` birlikte bir aralık verir.

Her biri için doğrulama, derleme, açıklama, şema ve mock LLM güncellendi. Derlenen SQL, bu
koşullar için de Go'da yazılmış referans hesapla karşılaştırılarak test ediliyor.

**Neden bu üçü.**
- Veri zaten var (`events.amount`, `users.signup_at`); seed değişmedi, 381 / 222 / 254 / 58
  referans sayıları ve onlara bağlı testler olduğu gibi kaldı.
- Format değişmedi, yalnızca beyaz liste büyüdü: yeni koşullar da sabit SQL kalıplarına derlenir
  ve her değer parametredir (metrik adı ve eşik dahil).
- "Şemaya uyan her kural anlamlı" ilkesi korundu: `en az 0` (herkesi seçer), aynı sınırın iki
  kez verilmesi ve birbirini dışlayan sınırlar (`en az 1000` + `en fazla 500`) doğrulama hatası.

**Bilinçli sınırlar.**
- Karşılaştırmalar kapsayıcıdır. "1000 TL üzeri" `en az 1000` olur; sınır değerin dahil olduğu
  pazarlamacıya yorum olarak gösterilir. Kesin "büyüktür" yok.
- "En fazla N kez" olay koşulunda yok (`exists: false` + `min_count` reddedilir); sipariş için
  `order_count lte` kullanılır.
- Metrikler kategoriye göre ayrılamaz ("elektronikte 1000 TL üzeri").
- Metrikler her sorguda olay tablosundan hesaplanır. Seed boyutunda sorun değil; büyük veride
  kullanıcı başına özet tablo gerekir (DECISIONS 4'teki ödünleşim).

**Hâlâ yok** (README "Sonraya bırakılanlar"): tüm kullanıcılara gönderim, iç içe VE / VEYA
grupları, ülke, sadakat seviyesi, cihaz, ürün görüntüleme, uygulama / email açma.

---

## 26. Kitle kuralı formla düzenlenir; JSON yalnızca geliştirici görünümünde

**Bağlam.** Aşama 5'te kuralı elle değiştirmenin yolu bir JSON metin kutusuydu. Kuralın
düzenlenebildiğini göstermek için ucuz bir yoldu, ama teknik olmayan bir pazarlamacı için
kullanılabilir değildi.

**Karar.**
- Kitle ekranında "Koşulları düzenle" bir form açar: her koşul bir satırdır; şehir, dil,
  cinsiyet, kategori, olay ve metrik listeden seçilir, gün / tutar / sayı için sayı kutusu
  vardır; koşul eklenir ve kaldırılır; "hepsini / en az birini" seçilir.
- "Uygula ve yeniden say" taslağı backend'e yollar. Kural aynı doğrulayıcıdan geçer; kabul
  edilirse açıklama ve kişi sayıları güncellenir, reddedilirse hatalar ilgili satırın altında
  görünür ve önceki kural yerinde kalır.
- Seçenek listeleri ve sınırlar `GET /api/segments/options` ile backend'den gelir
  (`segment.RuleOptions`, doğrulayıcının kullandığı tablolardan üretilir).
- JSON görünümü silinmedi; adres `?debug` ile açılırsa görünür.

**Brief varken form neden var.** İkisi farklı işler görür ve aynı kuralı üretir:
- Brief başlamak içindir: tek cümleden taslak çıkar; pazarlamacının veri modelini bilmesi
  gerekmez. Aynı cümleden içerik adımı ton ve teklifi de alır; form bunları taşıyamaz.
- Form düzeltmek içindir: "30 gün"ü "45" yapmak için brief'i yeniden yazıp modele sormak yavaş
  ve belirsizdir (model her seferinde aynı kuralı üretmeyebilir).
- Case'in tarifi de bu: "gerisini Claude'un önerdiği ve son kararı insanın verdiği bir araç".
  Düzeltme ne kadar kolaysa insan kararı o kadar gerçektir.

**Neden bu tasarım.**
- Liste değerleri arayüze gömülmedi. Backend'e yeni bir şehir ya da kategori eklenirse form onu
  kendiliğinden gösterir; formun sunduğu her değerin doğrulayıcıdan geçtiği testle sınanıyor.
- Arayüz geçerlilik kararı vermez. Tek tek alanlar listeden geldiği için geçersiz olamaz; ama
  kombinasyonlar (çelişen koşullar, aynı sınırın iki kez verilmesi) yalnızca backend'de
  yakalanır. Bu yüzden değişiklik "uygula" ile gider ve hatalar oradan gelir; tarayıcının kendi
  doğrulama baloncukları kapalıdır ki her sorun aynı yerden, aynı dille bildirilsin.
- Taslak bir kopyadır: backend kabul edene kadar ekrandaki kural ve sayılar değişmez.

**Bedeli / sınırı.**
- Elle düzenlenen kuralda modelin "Yorum" satırları kaybolur; yorumlar modelin kendi kuralına
  aitti ve değişen kural için geçerli olmayabilir.
- İçerik brief'ten üretildiği için, kural formla çok değiştirilirse metin kitleye uymayabilir
  (ör. brief "sepete ekleyenler" derken kural "yeni üyeler"e çevrilirse). Uyarı gösterilmiyor;
  pazarlamacı metni içerik adımında görür ve düzenler.
- Form için bileşen testi yok; mantığı (`lib/rule.ts`) ve store birim testleriyle, ekranın
  kendisi tarayıcıda elle denendi.

**Alternatif.** Düzenlemeyi tamamen kaldırmak; kitle beğenilmezse brief değiştirilip yeniden
üretilir. En az iş ve en sade ekran; ama küçük bir değişiklik için modele yeniden sormak gerekir.

---

## 27. Zamanlanmış gönderim ve iptal: zamanlayıcı yok, kuyruk zaten bekletiyor

**Karar.**
- Onayda isteğe bağlı bir `send_at` verilir (en fazla 30 gün sonrası). Kampanya ve alıcıları yine
  onay anında, tek transaction'da kuyruğa girer; yalnızca mesajların `next_attempt_at` değeri
  `send_at` olur. Worker zamanı gelmemiş satırı zaten almadığı için ayrı bir zamanlayıcı, cron ya
  da kampanya durumu yoktur.
- **Kitle onay anında dondurulur**, gönderim anında yeniden hesaplanmaz.
- Kampanya iptal edilebilir (`POST /api/campaigns/{id}/cancel`): bekleyen mesajlar `cancelled`
  olur ve bir daha gönderilmez. Gönderim başladıktan sonra iptal yalnızca kalanları durdurur.

**Neden kitle onayda dondurulur.**
- Pazarlamacının onayladığı sayı ile gönderilen sayı aynıdır. Gönderim anında hesaplansaydı
  onay ekranındaki sayı bir tahmin olurdu; "son kararı insan verir" ilkesi zayıflardı.
- İzin zaten gönderim anında yeniden kontrol ediliyor (DECISIONS 22): arada izni ya da adresi
  kalkan kişi atlanır. Yani en önemli tazelik kuralı korunuyor.
- Mevcut kuyruk tasarımı aynen kullanılır: retry için var olan `next_attempt_at` alanı
  zamanlamayı da taşır. Yeni bir hareketli parça yok.

**İptal nasıl güvenli kalıyor.** Üç durum var ve üçü de testle sınanıyor:
- *Bekleyen mesaj:* `Cancel` tek sorguyla `cancelled` yapar.
- *O anda provider'a gönderilmekte olan mesaj:* geri çağrılamaz. Başarılı olursa `sent` kalır;
  başarısız olup yeniden denenecekse worker onu kuyruğa geri koymak yerine `cancelled` yapar.
- *Çökmüş bir worker'dan kalan, kirası dolmuş mesaj:* yeniden alındığında kampanyanın iptal
  edildiği görülür ve gönderilmeden kapatılır.

**Bedeli / sınırı.**
- Kitle gönderim gününde eskimiş olabilir: onaydan sonra satın alan biri yine "sepetini
  unutma" mesajı alır, sonradan kurala uyan biri almaz. Bu yüzden zamanlama 30 günle sınırlı.
- İçerik de onay anındaki haliyle gider; zamanlanmış bir kampanyanın metni ya da saati
  sonradan düzenlenemez. Değiştirmek için iptal edip yeniden oluşturmak gerekir.
- Gönderim başladıktan sonra iptal edilen kampanyada mesajların bir kısmı gitmiş olur; pano
  kaç mesajın gittiğini ve kaçının iptal edildiğini gösterir.
- Saat dilimi tarayıcıdan gelir: pazarlamacı kendi yerel saatini seçer, arayüz bunu UTC'ye
  çevirip yollar. "Alıcının yerel saatinde gönder" yok.
- Sayfa yenilenince pano kaybolur (bilinen eksik); zamanlanmış bir kampanyanın panosuna
  sonradan dönmenin yolu arayüzde yok, `GET /api/campaigns/{id}` ile bakılabilir.

**Alternatif.** Kitleyi gönderim anında hesaplamak: onayda yalnızca kural ve içerik saklanır,
zamanı gelince kitle o anki veriyle kuyruğa alınır. Kitle her zaman günceldir; ama pazarlamacı
kaç kişiye gideceğini onaylarken bilemez ve zamanı gelen kampanyaları bulup kuyruğa alan ayrı
bir zamanlayıcı ile kampanya durumu gerekir.

---

## 28. Brief'teki gönderim zamanı öneri olarak okunur

**Karar.** Brief "cuma sabahı gönder" ya da "15 Kasım 09:30" gibi bir zaman içeriyorsa, kitle
çağrısında Claude bunu da çıkarır (`result.schedule`: `local_time`, `phrase`, `interpretation`).
Arayüz onay ekranındaki "Zamanla" alanını bu değerle önceden doldurur ve nereden okunduğunu
gösterir. Kampanya yalnızca pazarlamacı onaylarsa zamanlanır; alanı değiştirebilir ya da
"Hemen"i seçebilir.

**Nasıl güvenilir tutuluyor.**
- **Model saat dilimi hesabı yapmaz.** Tarayıcı kendi saat dilimini yollar; backend modele
  pazarlamacının o anki yerel tarih ve saatini (ve haftanın gününü) verir. Model zamanı aynı
  yerel saatte, dilimsiz yazar (`2026-11-13T09:00`); UTC'ye backend çevirir.
- **Çıktı doğrulanır.** Biçim, gelecekte olması ve 30 gün sınırı backend'de kontrol edilir;
  hatalı zaman diğer doğrulama hataları gibi modele geri verilir.
- **Öneri kuralı asla düşürmez.** Son denemede kural geçerli, yalnızca zaman hatalıysa kural
  önerisiz döner. İsteğe bağlı bir kolaylık, çekirdek akışı bozmamalı.
- **Yorum gösterilir.** "Sabah" için 09:00 gibi varsayımlar `interpretation` alanında yazılır ve
  ekranda görünür; kitle ekranındaki "Yorum" satırlarıyla aynı fikir (DECISIONS 14).

**Neden model, neden sabit kurallar değil.** Serbest ifadeleri ("cuma sabahı", "haftaya salı
öğleden sonra") sabit kalıplarla yakalamak kırılgan: tanınmayan bir yazım sessizce boş kalır.
Yanlış bir öneri ise zararsızdır, çünkü alan dolu gelir ve pazarlamacı görüp düzeltir.

**Bedeli / sınırı.**
- Tarih hesabı modelin en çok yanıldığı işlerden biri. Bugünün tarihi ve günü verildiği için
  risk azalır ama kalkmaz; bu yüzden değer hiçbir zaman onaysız kullanılmaz.
- **Gerçek Claude ile denenmedi.** Mock LLM yalnızca birkaç kalıbı tanır: "yarın", "N gün
  sonra", haftanın günü, gün + ay; saat için "saat 10", "09:30", sabah / öğle / akşam.
- Saat dilimi gelmezse `Europe/Istanbul` varsayılır.
- Kural formla düzenlenirse öneri kalır; brief yeniden gönderilirse yeniden hesaplanır.
- "Son 30 günde" gibi geriye dönük pencereler gönderim zamanı değildir; prompt bunu ayırır ve
  mock'ta "3 gün sonra gönder" ifadesinin kitle penceresi sanılmadığı testle sınanır.

**Alternatif.** Tarihi backend'de sabit kalıplarla aramak. Sonuç kesin ve test edilebilir, model
hatası riski yok; ama yalnızca öngörülen yazımları tanır.

---

## 29. Kampanyalar ekranı; adres çubuğu yönlendirici kütüphanesi olmadan

**Bağlam.** Zamanlanmış gönderimle (27) birlikte bir boşluk açıldı: yarım saat ve bir hafta
sonrasına planlanan iki kampanya veritabanında duruyor ve saatinde gidiyordu, ama arayüzde
yalnızca az önce onaylanan kampanyanın panosu görülebiliyordu; sayfa yenilenince o da
kayboluyordu.

**Karar.**
- `GET /api/campaigns` son 100 kampanyayı en yenisi üstte döner: brief, başlangıç zamanı, mesaj
  sayıları (iki kanalın toplamı) ve durum: `scheduled`, `sending`, `done`, `cancelled`.
- Arayüzde "Kampanyalar" ekranı bu listeyi gösterir ve ekranda kaldıkça 5 saniyede bir yeniler.
  Bir satıra tıklamak o kampanyanın mevcut panosunu açar; zamanlanmış olan oradan iptal edilir.
- Ekranın yeri adres çubuğunun hash kısmında tutulur: `#/kampanyalar`, `#/kampanya/12`, hash yok
  (oluşturma akışı). Sayfa yenilenince ya da bağlantı paylaşılınca aynı ekran açılır; geri /
  ileri tuşları çalışır.

**Neden.**
- **Durum backend'de türetilir**, arayüzde değil: liste, pano ve `/metrics` aynı satırları sayar;
  "zamanlandı" tanımı (başlangıç ileride ve hiçbir mesaj kuyruktan çıkmamış) tek yerde durur.
- **Yeni bir pano yazılmadı.** Listeden açılan kampanya, onaydan sonra görülen panoyla aynı
  bileşeni ve aynı canlı akışı (23) kullanır.
- **Yönlendirici kütüphanesi eklenmedi.** Üç adres için `lib/route.ts` içindeki iki saf fonksiyon
  (hash'i oku, hash'i yaz) yeterli ve test edilebilir. Hash kullanıldığı için nginx tarafında
  ayar gerekmez.
- Liste açılırken oluşturulmakta olan kampanya (brief, kitle, metinler) atılmaz; geri dönülünce
  kaldığı yerden devam eder.

**Bedeli / sınırı.**
- Sayfalama, arama ve filtre yok; liste 100 kampanyayla sınırlı. Bir arşiv değil, son ve
  zamanlanmış kampanyaları izleme ekranı.
- Liste sorgusu her kampanya için mesajları sayar; kampanya ve mesaj sayısı büyüdükçe pahalanır.
  O noktada sayılar kampanya satırında tutulur ya da sorgu sayfalanır.
- Liste yenilemesi SSE değil, 5 saniyelik sorgulamadır: iki desen birlikte var. Pano tek bir
  kampanyayı canlı izler; liste için her kampanyaya ayrı akış açmak gereksiz olurdu.
- Oluşturma akışının kendisi (brief, kitle, içerik adımları) adreste tutulmaz; o adımlardayken
  sayfa yenilenirse akış baştan başlar.
- Kimlik doğrulama yok: adresi bilen herkes her kampanyayı görür ve iptal edebilir. Case'in
  kapsamında kullanıcı yönetimi yoktu; gerçek bir üründe ilk eklenecek şey bu olurdu.

**Alternatif.** `vue-router` eklemek. Standart çözüm ve büyüyen bir uygulamada doğru tercih;
ama üç adres için bir bağımlılık ve yapılandırma getirir.

---

## 30. Dile göre içerik: ana dil için bir çağrı, diğer dillerin tamamı için bir çağrı

**Bağlam.** Karar 17 içeriği tek dilde bırakmıştı: seed'deki `language = en` kullanıcılar da
Türkçe metni alıyordu. Bonus ele alınırken iki soru konuşuldu: kim hangi dilde alır ve bunun
için model kaç kez çağrılır. İlk öneri dil başına ayrı çağrıydı; dil sayısı arttıkça çağrı
sayısının ve maliyetin de artacağı gerekçesiyle reddedildi.

**Karar.**
- **Her alıcı yalnızca kendi dilindeki metni alır** (`users.language`). Kimseye iki dilde mesaj
  gitmez.
- **Hangi dillerde metin yazılacağını kitle belirler:** kitlenin ulaşılabilen kısmının okuduğu
  diller (`audience.Counter.Languages`, kitle yanıtındaki `languages`). Kitlede hiç İngilizce
  okuyan yoksa İngilizce metin yazılmaz.
- **İçerik, dil sayısından bağımsız olarak iki model çağrısıyla üretilir:**
  1. Ana dil, yani kitlede en çok kişinin okuduğu dil için mevcut akışlı üretim
     (`Generator.GenerateIn`). Pazarlamacı okur, gerekirse düzenler.
  2. Pazarlamacı istediğinde geri kalan dillerin **tamamı tek çağrıda** yazılır
     (`Generator.Localize`, `POST /api/content/localize`). Model brief'ten yeniden yazmaz;
     pazarlamacının son haline getirdiği ana metni diğer dillere uyarlar.
- Kampanyanın içeriği dil başına bir set olarak saklanır:
  `{"default": "tr", "variants": {"tr": {...}, "en": {...}}}` (`content.Localized`; migration
  005 eski kampanyaları bu biçime sarar). Worker gönderim anında alıcının diline göre seti
  seçer; kampanyada o dil yoksa varsayılan dildeki seti gönderir.
- Her dil aynı doğrulamadan geçer (SMS 160, konu 80, izinli değişkenler, en kötü durum
  ölçümü); onayda hepsi yeniden doğrulanır. Hata yolları dili taşır (`variants.en.sms`).

**Neden.**
- **Çağrı sayısı sabit.** Kitlede beş dil olsa da iki çağrı yapılır; dil başına çağrıda bu
  beş olurdu. Maliyet dil sayısıyla değil, üretilen metin miktarıyla büyür.
- **İkinci çağrı onaylanan metinden yazılır.** Diller aynı teklifi anlatır ve pazarlamacının
  ana metinde yaptığı düzeltme diğerlerine de yansır. Her dil brief'ten bağımsız yazılsaydı iki
  dil farklı şeyler söyleyebilirdi.
- **İkinci çağrı pazarlamacının isteğiyle yapılır.** Ana metin beğenilmeyip yeniden
  üretildiğinde çeviriye boşuna token harcanmaz.
- **Kitlede olmayan dil yazılmaz.** Kimsenin almayacağı bir metin için ne model çağrılır ne
  pazarlamacıdan onay istenir.
- **Varsayılan dile düşme.** Pazarlamacı diğer dili yazdırmazsa ya da ileride kapalı kümeye
  metni olmayan bir dil eklenirse kimse mesajsız kalmaz; davranış bu karardan öncekiyle aynıdır.
- **Dil gönderim anında okunur,** izin ve adres gibi (22). `messages` tablosuna dil kolonu
  yine eklenmedi (17'deki gerekçe sürüyor): hangi setin gittiği kampanya ve kullanıcıdan türetilir.
- **Dil kümesi kapalı.** `content.Languages` (`tr`, `en`) `users.language` kısıtıyla aynı
  kümedir; bir test ikisinin örtüştüğünü kontrol eder.

**Bedeli / sınırı.**
- Tek çağrının bedeli: dillerden biri limiti aşarsa yanıtın tamamı reddedilir ve model bütün
  dilleri yeniden yazar. İki dilde önemsiz; çok dilde yeniden deneme pahalanır ve tek yanıt
  uzar. O noktada diller birkaç gruba bölünür.
- Ana metin çeviriden sonra değiştirilirse diğer diller eskir. Arayüz bunu fark edip uyarır
  ("yeniden yaz"), ama zorlamaz; backend bu ilişkiyi bilmez.
- Diğer diller yazdırılmadan da onaylanabilir; o alıcılar ana dildeki metni alır. Arayüz kaç
  kişi olduğunu onaydan önce söyler.
- Doğrulama limiti ve değişkenleri kontrol eder, anlamı değil: pazarlamacı okuyamadığı bir
  dildeki metni onaylıyor olabilir.
- Mock LLM çeviri yapmaz; her dil için brief'ten sabit metnini yazar. Ana metne elle yapılan
  değişiklik mock'ta diğer dile yansımaz. Gerçek Claude ile denenmedi.
- Pano dil bazında sayı göstermez; yalnızca kampanyanın hangi dillerde metni olduğunu söyler.
- Zamanlanmış bir kampanyada, onay ile gönderim arasında dilini değiştiren alıcı yeni dilindeki
  metni alır.

**Alternatifler.**
- *Dil başına ayrı çağrı (paralel).* Bir dildeki hata diğerlerini etkilemez ve her dil ayrı
  izlenir; ama çağrı sayısı dil sayısıyla artar. Maliyet gerekçesiyle reddedildi.
- *Bütün diller tek çağrıda, ana dil dahil.* Toplam bir çağrı; ama pazarlamacı ana metni
  düzelttiğinde diğer diller yine yeniden yazdırılmalı, yani ikinci çağrı ortadan kalkmaz, ilk
  yanıt ise uzar ve akışı izlemek zorlaşır.
- *Onaydan sonra arka planda çeviri.* Pazarlamacının görmediği bir metin gönderilmiş olurdu;
  "insan düzenler ve onaylar" akışına aykırı.
