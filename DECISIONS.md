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
