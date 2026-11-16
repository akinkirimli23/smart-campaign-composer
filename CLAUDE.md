# CLAUDE.md

> **Gereksinimlerde şüphe olursa `case.pdf`'e bak** (repo kökünde, `.gitignore`'da; asla commit edilmez).

Insider One AI Developer case: **Smart Campaign Composer**. Pazarlamacı doğal dille bir brief
yazar, Claude bundan segment kuralı ve SMS / email içeriği önerir, insan düzenleyip onaylar,
mesajlar kuyruk üzerinden mock provider'a gönderilir, gönderim canlı izlenir.

**Teslim: 17 Kasım 2026.** Değerlendirme: LLM entegrasyonu %25, mimari ve kararlar %20,
backend güvenilirliği %20, frontend %20, test ve kod kalitesi %15.
Case'in kapsam notu: her şeyi yapmak yerine çekirdek akışı sağlam yap, yetişmeyeni README'de belirt.

## Önce oku

- `DECISIONS.md`: alınmış kararlar ve gerekçeleri. Bir kararı değiştirirsen orayı güncelle,
  eski kaydı silme, nedenini yaz. Başındaki "Hızlı bakış" tablosunda her kararın durumu var:
  yeni karar eklersen ya da birini değiştirirsen o satırı da güncelle. Numaralar koddan ve
  dokümanlardan başvurulduğu için sabittir.
- `README.md`: kurulum, mimari özeti, testler, "Bilinen eksikler" ve "Sonraya bırakılanlar".
  Yeni bir eksik ya da ertelenen iş çıkarsa oraya yaz.

## Komutlar

```bash
docker compose up --build        # Postgres + init (migration + seed) + mock provider (:8090) + api (:8080) + arayüz (:3000)
make test                        # backend (geçici Postgres'e karşı) + frontend (vitest) testleri, container içinde
make reset                       # DB volume'unu sil, sonraki up sıfırdan seed eder
cd backend && go test ./...      # yerel; DB testleri TEST_DATABASE_URL yoksa atlanır
```

Compose'taki Postgres host'ta **5433** portunda (yerel bir Postgres ile çakışmasın diye).
Yerel entegrasyon testi için:
`TEST_DATABASE_URL=postgres://scc:scc@localhost:5433/scc?sslmode=disable go test ./...`

## Değişmez kurallar

Bunlar case gereksinimi ya da bilinçli karar. Bozan bir değişiklik yapma.

1. **LLM çıktısı asla SQL olarak çalıştırılmaz.** Claude yapılandırılmış JSON üretir; backend
   şemaya göre doğrular ve parametreli SQL'e kendisi derler. Alan adları, operatörler ve
   değerler beyaz listeden gelir; kullanıcıdan / LLM'den gelen hiçbir metin SQL'e string olarak
   eklenmez.
2. **Şehir, kategori ve sadakat seviyesi kapalı küme.** Geçerli değerler `seed.Cities()`, `seed.Categories` ve `seed.LoyaltyTiers`.
   Kümede olmayan değer doğrulama hatasıdır, sessizce sıfır kişilik kitle değildir.
3. **İzin + adres birlikte.** Bir kanaldan gönderim için hem `*_opt_in = true` hem adres
   (`email` / `phone`) dolu olmalı. Kitle sayıları kanal bazında bu kuralla raporlanır.
4. **Karakter limitleri kişiselleştirme sonrası ölçülür.** SMS ≤ 160, email konusu ≤ 80.
   `{{first_name}}` gibi değişkenler doldurulduktan sonraki en kötü durum (en uzun değer)
   kontrol edilir. Karakter sayısı byte değil rune ile.
5. **Aynı kişiye aynı mesaj iki kez gitmez.** Veritabanı kısıtıyla garanti edilir
   (`UNIQUE (campaign_id, user_id, channel)`), provider'a idempotency key gönderilir.
6. **Kuyruk Postgres tablosu.** `SELECT … FOR UPDATE SKIP LOCKED`. Redis yok; yatay ölçeklemede
   ilk adım hız limitini Redis'e taşımak (DECISIONS 2 ve 3).
7. **Testler gerçek API key olmadan çalışır.** LLM bir interface arkasında; testler mock ile.
   `LLM_MODE=auto` iken key yoksa mock kullanılır.
8. **Seed deterministik.** Aynı `SEED_USERS` / `SEED_RANDOM` her makinede aynı veriyi üretir.
   Varsayılan veride örnek brief 381 kişi verir (222 SMS, 254 email, 58 ulaşılamaz);
   segment derleyicisinin testleri bu sayıyı referans alabilir. Şehirler 81 il (DECISIONS 39):
   İstanbul dışındakilerin bir kısmı `seed.resettle` ile diğer illere taşınır; bu, üreteçten sayı
   çekmez ve İstanbul'a dokunmaz. Şehir dağılımını değiştirirken bu ikisini koru, yoksa
   referans sayılar kayar.

## Kod düzeni

- Go 1.25 (Anthropic SDK v2 gereği, DECISIONS 16), standart `net/http` (Go 1.22 route pattern'leri), `pgx/v5`, `log/slog`.
  Gereksiz bağımlılık ekleme; ekleyeceksen DECISIONS'a nedenini yaz.
- `backend/cmd/*` giriş noktaları, `backend/internal/*` paketler.
- Migration'lar `backend/internal/db/migrations/NNN_ad.sql`, binary'ye gömülü, sıralı.
  Uygulanmış migration'ı düzenleme, yenisini ekle.
- DB gerektiren testler `testdb.New(t)` kullanır (her test için ayrı şema).
- Frontend: Vue 3 (`<script setup>`), TypeScript (strict), Pinia, Vite, vitest. Yönlendirici ve
  arayüz kütüphanesi yok; bir açılış sayfası, dört adımlı oluşturma akışı ve bir kampanyalar
  listesi. Ekranın yeri adres çubuğunun hash'inde tutulur (`lib/route.ts`, DECISIONS 29): hash yok
  = açılış, `#/olustur`, `#/kampanyalar`, `#/kampanya/N`. Tek ek bağımlılık iki gömülü yazı tipi
  paketi (DECISIONS 32).
- Kod yorumları İngilizce, dokümanlar (README, DECISIONS) Türkçe.
- `.env` asla commit edilmez. `ai-sessions/` içine koymadan önce API key ve kişisel bilgileri temizle.

## Durum ve plan

İş gün bazlı değil, aşama bazlı ilerler. Bir aşamanın "bitti" ölçütlerinin hepsi sağlanınca
sonrakine geçilir.

| # | Aşama | Kapsam | Bitti ölçütü | Durum |
|---|---|---|---|---|
| 1 | İskelet | compose, şema (`users`, `events`), deterministik seed, `/healthz`, `/readyz` | Compose temiz kurulumda kalkıyor, testler geçiyor, örnek brief SQL ile 381 / 222 / 254 / 58 veriyor | ✅ |
| 2 | Kitle | Segment kuralı JSON şeması, doğrulayıcı, parametreli SQL derleyici (**zaman parametre olarak verilir**, SQL'de `now()` yok), kanal bazlı kitle sayısı endpoint'i, mock LLM | Örnek brief sabit bir tarihte 381 / 222 / 254 / 58 veriyor; geçersiz şehir, bilinmeyen alan ve SQL enjeksiyonu denemesi testle reddediliyor | ✅ |
| 3 | İçerik | Akışlı (SSE) SMS / email üretimi, değişkenler doldurulduktan sonra karakter limiti kontrolü, hatalı çıktıda hatayı Claude'a geri verip sınırlı sayıda yeniden deneme | Mock LLM'in bozuk JSON, limiti aşan SMS ve bilinmeyen değişken senaryoları testte doğru ele alınıyor | ✅ |
| 4 | Gönderim | `campaigns` / `messages` tabloları, worker, kanal başına 100/sn, retry + backoff, 429'da `Retry-After`'a uyma; hata, gecikme ve 429 döndüren mock provider | Testte aynı kişiye iki kez mesaj gitmiyor ve hız limiti aşılmıyor; mock provider `docker compose up` ile kalkıyor | ✅ |
| 5 | Frontend | Vue 3 + TS + Pinia: brief → kitle → içerik → onay → canlı pano akışı; metrics endpoint'i | Mock LLM ile tarayıcıda brief'ten canlı panoya uçtan uca bir kampanya gönderilebiliyor; panodaki sayılar metrics endpoint'iyle tutarlı; pazarlamacının elle düzenlediği metin onay anında backend'de yeniden doğrulanıyor (SMS 160, konu 80, yalnızca izinli değişkenler); arayüz karakter sayısını canlı gösteriyor | ✅ |
| 6 | Teslim | README, DECISIONS, CI (GitHub Actions), `ai-sessions/`, temiz kurulum denemesi, "Case gereksinimleri" listesinin madde madde kontrolü | Temiz bir clone'da README'deki adımlarla `docker compose up --build` ve testler çalışıyor; CI yeşil; `ai-sessions/` key ve kişisel bilgiden temizlenmiş; gerçek Claude ile en az bir uçtan uca deneme yapıldı ya da yapılmadığı README'de yazıyor; "Case gereksinimleri" listesindeki her madde işaretli ya da README'de eksik olarak belirtilmiş | ✅ |

### Sınırlar

- Aşama 1–4 en geç **15 Kasım akşamı** biter.
- **17 Kasım'da yeni özellik yok**; yalnızca aşama 6.
- Bir aşama uzarsa kapsamı daraltılır, kesilen kısım README'ye yazılır.

### Çalışma kuralları

- **Aşama sonu:** testler çalıştırılır, commit atılır, push'lanır ve kullanıcıya kısa bir özet verilir.
- **Karar kuralı:** DECISIONS'a girecek bir karar verilmeden önce kullanıcıya iki seçenek,
  artısı ve eksisiyle gösterilir; kullanıcı seçince devam edilir.

## Yeni oturum başlangıcı

1. `make test` çalıştır (make yoksa `docker compose --profile test run --rm --build test` ve
   `docker compose --profile test run --rm --build web-test`).
2. Yukarıdaki durum tablosuna bak; ilk ⏳ aşama sıradakidir.
3. O aşamanın bitti ölçütlerinden eksik olanlarla devam et.

## Case gereksinimleri

`case.pdf`'ten çıkarılmış kontrol listesi. Sütun: karşılanacağı aşama. ✅ = karşılandı.
Aşama 6'da teslimden önce madde madde kontrol edilir; karşılanmayan README'de belirtilir.

### Fonksiyonel

| Gereksinim | Aşama |
|---|---|
| Kullanıcı akışı 1: Claude brief'ten segment kuralı üretir, arayüz kaç kişiye gideceğini gösterir | 2 ✅, 5 ✅ |
| Kullanıcı akışı 2: SMS ve email içeriği ekrana stream edilerek akar | 3 ✅ (SSE), 5 ✅ |
| Kullanıcı akışı 3: pazarlamacı önizler, gerekirse düzenler ve onaylar | 5 ✅ |
| Kullanıcı akışı 4: mesajlar kuyruk üzerinden mock provider'a gönderilir | 4 ✅ |
| Kullanıcı akışı 5: dashboard gönderim durumunu canlı gösterir | 5 ✅ |
| Gerçek kimseye mesaj gitmez: alıcılar sahte kullanıcılar, gönderim mock provider'a | 1 ✅ (seed), 4 ✅ |
| Kitle: Claude'un segment kuralı backend'de doğrulanır, LLM çıktısı doğrudan SQL olarak çalıştırılmaz | 2 ✅ |
| Kitle: izni olmayan kullanıcıya o kanaldan mesaj gönderilmez | 2 ✅ (sayım), 4 ✅ (gönderim) |
| İçerik: SMS en fazla 160 karakter | 3 ✅, 4 ✅ (onayda yeniden doğrulama), 5 ✅ (arayüz) |
| İçerik: email konusu en fazla 80 karakter | 3 ✅, 4 ✅ (onayda yeniden doğrulama), 5 ✅ (arayüz) |
| İçerik: `{{first_name}}` gibi kişiselleştirme değişkenleri desteklenir | 3 ✅, 4 ✅ (gönderimde doldurma) |
| Gönderim: asenkron kuyruk | 4 ✅ |
| Gönderim: kanal başına saniyede en fazla 100 mesaj | 4 ✅ |
| Gönderim: aynı kişiye aynı mesaj iki kez gitmez | 4 ✅ |
| Gönderim: retry stratejisi tasarlanır (ve DECISIONS'ta gerekçelendirilir) | 4 ✅ (DECISIONS 21) |
| Takip: canlı gönderim durumu endpoint'i | 5 ✅ |
| Takip: metrics endpoint'i | 5 ✅ |
| Takip: health check endpoint'i | 1 ✅ |

### AI

| Gereksinim | Aşama |
|---|---|
| Claude'un çıktısı yapılandırılmış JSON, backend'de kontrol ediliyor (segment kuralı) | 2 ✅ |
| Claude'un çıktısı yapılandırılmış JSON, backend'de kontrol ediliyor (içerik) | 3 ✅ |
| "Çağırmak yeterli değil, güvenilir hale getir": hatalı çıktıda hata geri beslenip sınırlı yeniden deneme | 3 ✅ |
| Testler gerçek API key olmadan, mock bir LLM ile çalışıyor | 2 ✅, 3 ✅ (her aşamada korunur) |

### Kaynaklar

| Gereksinim | Aşama |
|---|---|
| API key `.env`'deki `ANTHROPIC_API_KEY`'den okunuyor | 1 ✅ (config), 3 ✅ (kullanım) |
| Repoda `.env.example` var | 1 ✅ |
| Birkaç bin kullanıcılık seed (ad, email, telefon, şehir, alışveriş geçmişi, kanal izinleri) | 1 ✅ |
| Mock SMS / email provider gerçek bir servisi taklit ediyor | 4 ✅ |
| Mock provider zaman zaman hata, gecikme ve 429 döndürüyor | 4 ✅ |
| Frontend Vue.js, backend Go | 1 ✅ (Go), 5 ✅ (Vue) |

### Teslim edilecekler

| Gereksinim | Aşama |
|---|---|
| GitHub repository | 1 ✅ |
| `docker compose up` ile tek komutla kurulum, seed verisi dahil | 1 ✅ |
| `docker compose up` ile tek komutla kurulum, mock provider dahil | 4 ✅ |
| `make test` ile çalışan testler | 1 ✅ (her aşamada korunur) |
| `README.md`: kurulum ve kısa mimari özeti | 6 ✅ |
| `DECISIONS.md`: önemli kararlar ve nedenleri | her aşama ✅ (45 kayıt) |
| `ai-sessions/`: tüm Claude session export'ları, API key ve kişisel bilgiden temizlenmiş | ✅ dört dosya. Üçüncü oturum iki dosyada: 13 Kasım'a kadarı ve devamı (16 Kasım akşamına kadar: ince ayar, gerçek Claude denemesi, herkese gönder, giden metin + CSV, sadakat seviyesi). Sonrasında çalışılırsa devam dosyası yeniden alınmalı |
| Yetiştirilemeyenler README'de belirtilmiş | 6 ✅ ("Bilinen eksikler", "Sonraya bırakılanlar") |

### Bonus (isteğe bağlı)

| Gereksinim | Aşama |
|---|---|
| Zamanlanmış gönderim | ✅ iptal ile birlikte (DECISIONS 27) |
| A/B varyantları | yapılmadı; nedeni ve nasıl yapılacağı DECISIONS 31 ve README "Bonuslar"da |
| Kullanıcının diline göre içerik | ✅ ana dil için bir çağrı, diğer dillerin tamamı için bir çağrı (DECISIONS 30) |
| CI pipeline | 6 ✅ |

## Segment kuralı (aşama 2, kesinleşti)

Format ve gerekçesi DECISIONS 13–15'te. Kısaca:

- `internal/segment`: `Parse` (katı ayrıştırma + doğrulama), `Compile(rule, asOf)` (parametreli
  SQL), `Describe` (pazarlamacıya gösterilen açıklama, kuraldan türetilir), `JSONSchema`
  (Claude'a verilen şema, aynı tablolardan üretilir), `Generator` (LLM + hata geri besleme, en
  fazla 3 deneme).
- Koşul tipleri: `attribute` (şehir, dil, cinsiyet, sadakat seviyesi, yaş), `event` (sepet / satın alma, isteğe
  bağlı `min_count` ve `max_count`), `signup` (üyelik tarihi), `metric` (`total_spent`,
  `order_count`, `avg_order_value`; isteğe bağlı `category`). Son üçü DECISIONS 25 ile eklendi;
  `max_count`, ortalama ve metrik kategorisi DECISIONS 40 ile. Alışverişi olmayanın ortalaması
  yoktur: `avg_order_value` koşuluna hiçbir yönde girmez (SQL'de `COALESCE(…, false)`).
- `group` (DECISIONS 38): kendi `match`'iyle bağlı düz koşullar; kuralın tek bir koşulu sayılır.
  Derinlik en fazla iki (grup içinde grup reddedilir), grubun bağlacı kuralınkinin tersi olmalı,
  en az iki üye. Doğrulama her listeyi ayrı denetler (`validateTogether`); derleyici grubu
  parantezli tek ifade yapar (`compileLeaf` + `join`); şema üyeler için düz koşul şemalarını
  yeniden listeler, özyineleme yok. Daha derin iç içelik ekleme: açıklama, form ve model şeması
  iki seviyeye göre kuruldu.
- Yeni bir alan / operatör eklemek: `rule.go` tabloları, `validate.go`, `compile.go`,
  `describe.go`, `schema.go` ve testler. Tablolar tek kaynak; SQL'e giden kolon adı oradan gelir.
- Claude'un çıktısı ya `rule` ya `unsupported`. Açıklama metni Claude'dan alınmaz; Claude
  yalnızca `assumptions` (yorumlar) döner.
- `rule` yanıtında isteğe bağlı `schedule` (DECISIONS 28): brief'teki gönderim zamanı, öneri
  olarak. `Generator.GenerateAt(ctx, brief, Clock)` pazarlamacının yerel saatini modele verir;
  `Generate` saatsiz çağırır ve zaman aranmaz. Hatalı zaman yeniden denenir ama son denemede
  kuralı düşürmez (`parseOutcome` zaman hatalarını ayrı döner). Model yerel saat yazar, UTC'ye
  backend çevirir.
- `internal/audience`: kanal bazlı sayım. `SMSReachable` / `EmailReachable` sabitleri "bu kanaldan
  gönderilebilir" tanımının tek yeri: izin + adres + bastırılmamış (`suppressions` tablosu,
  DECISIONS 35). Sayım, kuyruğa alma ve worker hep bunları kullanır; yeni bir "gönderilemez"
  nedeni eklenecekse buraya eklenir, ayrı bir kontrol yazılmaz.
- `internal/llm`: `Client` ve `Streamer` arayüzleri, testler için `Scripted`; `llm/mockllm` key
  yokken kullanılan anahtar kelime eşleştirici (tanımadığı niteliği sessizce atmaz, `refusals`
  tablosuyla reddeder: DECISIONS 36; uç durum seti `scripts/brief-seti.py`, `docs/brief-seti.md`); `llm/anthropicllm` gerçek istemci (resmi Go SDK,
  DECISIONS 16). SDK'yı yalnızca `anthropicllm` import eder. Testleri yerel bir SSE sunucusuna
  karşı çalışır, key istemez.
- **Modele giden şema kayıtlı kural biçiminden iki yerde ayrılır (DECISIONS 41).** Gerçek API
  büyük şemayı reddeder ("compiled grammar is too large"), o yüzden: (1) düz koşul şemaları
  `$defs` altında bir kez tanımlanır, kural ve grup `$ref` ile gösterir; (2) modelin biçiminde
  null yok: kategori için `"any"`, `min_count` / `max_count` ve metriğin `within_days`'i için
  `0` yazar, `fromModel` bunları `Parse`'tan önce null'a çevirir. Bu işaret değerleri yalnızca
  model yolunda geçerlidir; `Parse` ve API hâlâ null ister.
- **Şemada alan sırası anlam taşır.** Kısıtlı çıktı anahtarları şemadaki sırayla yazar; Go map'i
  alfabetik sıralar. Bu yüzden `objectSchema` sıralı `field` listesi alır ve her seçenek ayırt
  edici alanıyla (`kind`, `type`) başlar. Map'e geri dönme: `kind` başta değilken gerçek model
  yalnızca "desteklenmiyor" diyebiliyordu.
- "Veya" ile bağlı bir listede aynı olayın "yaptı" ve "yapmadı" hali herkesi kapsıyorsa
  doğrulayıcı reddeder (`coversEveryone`, DECISIONS 42): gerçek model bunu "herkese gönder" ve
  çelişkili brief için arka kapı olarak kullandı. Koşulsuz kuralın yasak olmasıyla aynı karar.
- "Tüm kullanıcılar" kuralda açık bir işarettir (DECISIONS 43): `everyone: true` + boş koşul
  listesi, `WHERE TRUE` olarak derlenir; ulaşılabilirlik her kuraldaki gibi çağıranda uygulanır.
  İşaretsiz boş liste hâlâ hatadır; boş listeyi "herkes" sayan bir yol ekleme. Model şemasında
  `everyone` kuralın ilk alanıdır. Mock yalnızca başka koşul tanımadığında `everyone` döner.
  Arayüzde formun başındaki kutu ve kitle ekranındaki uyarı (`normaliseRule` koşulları göndermez).
- Sadakat seviyesi (DECISIONS 45): `users.loyalty_tier`, seed'de toplam alışveriş tutarından
  türetilir (`seed.TierFor`; üreteçten sayı çekmez, referans sayılar bu yüzden kaymadı).
  Eşikleri değiştirirsen migration `009`'daki doldurma ile aynı kalmaz; yeni migration yaz.
  Kuralda bir `setFields` satırıdır, harcama koşulu değil; modelin seviyeyi harcamaya çevirmesi
  prompt'ta yasak.
- Kitle yanıtındaki `loyalty` (`audience.Counter.Tiers`): ulaşılabilen kişilerin seviye
  dağılımı, her seviye sıfır da olsa listelenir; kitle ekranındaki "Sadakat seviyeleri" kartı.
- Modelin şemasında kısa kümeli nitelikler (dil, cinsiyet, sadakat seviyesi) **tek seçenekte**
  durur (`plainVariants`, `sharedSetMax`); şehir kendi seçeneğinde. Her niteliğe ayrı seçenek
  vermeye dönme: gerçek API şemayı reddeder. Alan ile değerin uyuşmasını `Parse` denetler.
- Reddedilen model çıktısı `segment output rejected` ile loglanır (deneme no + hatalar).
- Şemaya alan eklersen gerçek API'de dene (`LLM_MODE=auto` + key); sınıra yakın ve testler bunu
  göremez. Gerçek Claude ile deneme 16 Kasım'da yapıldı (README "Bilinen eksikler").

## İçerik (aşama 3, kesinleşti)

Kararlar DECISIONS 16–19'da. Kısaca:

- `internal/content`: `Content` (`sms`, `email.subject`, `email.body`; hepsi şablon), `Parse`
  (katı ayrıştırma + doğrulama), `Content.Validate` (model çıktısı ve elle düzenlenen metin için
  aynı kontrol; aşama 5'teki onay bunu çağırmalı), `Render` (aşama 4'te gönderimde doldurma),
  `Generator` (akışlı üretim + hata geri besleme, en fazla 3 deneme), `DBLengths` (değişkenlerin
  en uzun değeri).
- İzinli değişkenler `content.Variables` tablosunda; yeni değişken eklemek için tabloya satır
  eklemek yeter (prompt, doğrulayıcı ve uzunluk sorgusu oradan beslenir).
- Limit en kötü durumla ölçülür: her değişken `users` tablosundaki en uzun değeriyle sayılır.
- `POST /api/content/generate` SSE döner: `attempt`, `delta`, `rejected`, `result`, `error`.
  Yalnızca `result` doğrulanmış içerik taşır; `delta` önizlemedir.
- Mock LLM brief'te `#demo-retry` görürse ilk yanıtı bilerek geçersiz üretir (demo için).
- `messages` tablosunda `language` kolonu yok (DECISIONS 17, 30): gönderilen dil kampanya ve
  kullanıcıdan türetilir.
- **Dile göre içerik (DECISIONS 30).** Kampanyanın içeriği `content.Localized`
  (`default` + dil başına `Content`); `ParseLocalized` tek seti de kabul eder (varsayılan dil
  `tr`). Diller kapalı küme: `content.Languages`, `users.language` kısıtıyla aynı olmalı (bir
  test karşılaştırır).
  - Çağrı sayısı sabit, iki: `Generator.GenerateIn(…, language, …)` ana dili yazar (kitlede en
    çok ulaşılabilen kişinin dili, `audience.Counter.Languages`'in ilk satırı);
    `Generator.Localize(…, source, from, to, …)` kalan dillerin **hepsini tek çağrıda**, ana
    metinden yazar. Dil başına çağrı yapan bir yol ekleme.
  - `POST /api/content/localize` SSE döner; `delta` dilini taşır, `result` `variants` döner.
    Hata yolları: model yanıtında `en.sms`, onayda `variants.en.sms`.
  - Worker `claim` içinde `u.language` okur ve `Localized.For(dil)` ile seti seçer; set yoksa
    varsayılan dil. Yeni bir gönderim yolu eklersen seti aynı şekilde seç.
  - Mock LLM çevirmez; `content.localize` görevinde her dil için sabit metnini yazar.

## Gönderim (aşama 4, kesinleşti)

Kararlar DECISIONS 21–22'de. Kısaca:

- `internal/campaign`: `Store.Create` kampanyayı ve alıcılarını tek transaction'da yazar (kanal
  başına tek `INSERT … SELECT`, derleyicinin WHERE'i + `audience.*Reachable`); `Store.Status`
  kanal ve durum bazında sayar; `Store.List` son 100 kampanyayı durumlarıyla döner; `Store.Recipients` bir kampanyanın
  mesajlarını sayfa sayfa, sorunlular önce ve adresler maskeli olarak döner (DECISIONS 33; giden metin ve CSV: DECISIONS 44). Aşama 5'in canlı panosu ve metrics endpoint'i bunun üzerine kurulur.
- `internal/delivery`: `Worker` (kanal başına bir döngü; `FOR UPDATE SKIP LOCKED` ile kiralama,
  gönderimde şablon doldurma ve izin kontrolü, sonuç yazarken `status = 'sending' AND attempts = N`
  koşulu), `Limiter` (eşit aralıklı slot, burst yok, `PauseUntil` ile 429), `HTTPProvider`.
- Giden metin saklanır (DECISIONS 44): `Worker.finishSent`, sağlayıcının kabul ettiği
  doldurulmuş metni `sent` durumuyla aynı UPDATE'te `messages.sent_subject` / `sent_body`'ye
  yazar. Yalnızca teslim edilen mesajda doludur. Metni sonradan yeniden üretip gösteren bir yol
  ekleme; gösterilen her zaman saklanandır.
- Dışa aktarma: `GET /api/campaigns/{id}/recipients.csv`, `Store.EachRecipient` (tek sorgu,
  sayfasız). Liste ile aynı `recipientsFrom` ve `scanRecipient`'i kullanır; adresler maskeli
  kalır. Dosyaya açık adres yazan bir seçenek ekleme (kimlik doğrulama yok). Kullanıcıdan ya da
  modelden gelen hücreler `csvCell`'den geçer.
- Sağlayıcı `recipient_unreachable` koduyla ret verirse (`delivery.Unreachable`) worker kişiyi o
  kanal için bastırır (`Worker.suppress`); karar koda bakar, hata metnine değil. Başka bir kalıcı
  ret (`Permanent`) kimseyi bastırmaz.
- Abonelikten çıkma (DECISIONS 37): mock sağlayıcı teslim ettiği alıcıların bir kısmı için
  `POST /api/provider/events` çağırır (`reference` = bizim idempotency key'imiz, `X-Webhook-Secret`
  doğrulanır). `Store.Unsubscribe` yalnızca teslim edilmiş bir mesaj için kabul eder ve aynı
  `suppressions` tablosuna `unsubscribed` nedeniyle yazar; gerisi karar 35 ile aynı yoldan çalışır.
  `users.*_opt_in` değiştirilmez.
- Mesaj durumları: `pending` → `sending` → `sent` / `failed` / `skipped` / `cancelled`. Retry
  durumu satırda (`attempts`, `next_attempt_at`); 429 deneme hakkı harcamaz.
- Zamanlama (DECISIONS 27): `Store.Create(…, asOf, sendAt)`; mesajların `next_attempt_at` değeri
  `sendAt` olur, ayrı zamanlayıcı yok. Kitle onayda dondurulur. `Store.Cancel` bekleyenleri
  `cancelled` yapar; worker iptal edilmiş kampanyanın mesajını ne yeniden dener ne gönderir
  (`retry` içindeki CASE ve `deliver` başındaki kontrol). Worker'a yeni bir "kuyruğa geri koy"
  yolu eklersen iptali orada da kontrol et.
- Idempotency key: `campaign-<id>-user-<id>-<kanal>`; her denemede aynı.
- `POST /api/campaigns` onaydır: kural `segment.Parse`, içerik `content.Parse` ile yeniden
  doğrulanır. Aşama 5'teki arayüz bunu çağırır; ayrı bir doğrulama endpoint'i yok.
- Worker API süreci içinde çalışır (`WORKER_ENABLED`); hız sınırlayıcı süreç içi olduğu için
  gönderen süreç tek olmalı.
- `internal/mockprovider` + `cmd/mockprovider`: compose'ta `mock-provider` servisi, `:8090`.
  Oranlar `MOCK_*` ortam değişkenleriyle ayarlanır. `MOCK_UNREACHABLE_RATE` (varsayılan %2)
  adrese bağlı kalıcı rettir (`422`): "başarısız" durumunun çalışan sistemde görünmesini sağlar
  (DECISIONS 34).
- Segment kuralına `min_count`, harcama / sipariş metrikleri ve üyelik tarihi aşama 5'ten sonra
  eklendi (DECISIONS 25). "Herkese gönder" ve diğer eksikler README "Sonraya bırakılanlar"da.

## Arayüz (aşama 5, kesinleşti)

Kararlar DECISIONS 23–24'te. Kısaca:

- `frontend/src/stores/campaign.ts`: tüm akışın durumu ve eylemleri tek Pinia store'unda
  (`generateSegment`, `applyRule`, `generateContent`, `approve`, `reset`). Bileşenler yalnızca
  bu durumu çizer.
- `frontend/src/api/client.ts`: JSON istekleri, içerik akışı (`fetch` + `lib/sse.ts`; POST
  olduğu için `EventSource` kullanılamaz) ve canlı durum (`EventSource`).
- `frontend/src/lib/template.ts`: canlı sayaç için şablon ölçümü. Backend'deki
  `internal/content` ile aynı kurallar; birini değiştirirsen diğerini ve iki taraftaki testleri
  de güncelle. Yetki backend'dedir.
- Kitle kuralı formla düzenlenir (`components/RuleEditor.vue`, mantığı `lib/rule.ts`; DECISIONS
  26). Seçenekler `GET /api/segments/options`'tan gelir; arayüze değer listesi gömme. Geçerlilik
  kararı backend'de: taslak `applyRule` ile gider, hatalar satırın altında gösterilir. JSON
  görünümü yalnızca `?debug` ile. Tek koşulun formu `ConditionFields.vue`; grup, içinde aynı
  formların durduğu bir kutudur ve bağlacı sorulmaz (kuralınkinin tersi, `normaliseRule` zorlar).
- Uzun kümeler (81 il, DECISIONS 39) kutucukla değil `OptionPicker.vue` ile seçilir: seçilenler
  etiket, gerisi arama (`searchOptions`, `foldTr` `lib/rule.ts` içinde). Eşik seçenek sayısıdır.
  Brief, kitle ve mesaj adımlarında `BriefNote.vue` ile görünür kalır.
- Mock LLM şehirleri kelime olarak tanır (`wordCity`, `citiesIn`, `cityAliases`); alt dizi
  aramasına dönme: "müşteri" içinde Muş bulur.
- Diller (DECISIONS 30): `store.languages` kitle yanıtından gelir (etiketleriyle; arayüze dil
  listesi gömme). `store.content` ana dildeki metin, `store.variants` diğer diller,
  `store.activeLanguage` düzenlenen dil; `store.localize()` diğer dilleri tek istekte yazdırır.
  `contentValid` görünen dile değil bütün dillere bakar. `approve` her zaman
  `{default, variants}` gönderir. Ana metin sonradan değişirse `translationsStale`. Kitlenin bir
  dili için metin yokken onay kapalıdır (`languagesResolved`); açmanın tek yolu dilleri
  yazdırmak. "Ana dili herkese gönder" seçeneği bilerek yok: tek dilli kampanya kitleye dil
  koşulu ekler. Bu kural arayüzde; backend'in varsayılan dile düşmesi yalnızca güvenlik ağı.
- Brief'ten okunan gönderim zamanı (`segment.schedule`) store'da `scheduleSuggestion` olur ve
  "Zamanla" alanını önceden doldurur; pazarlamacı değiştirebilir. Öneri tek başına hiçbir şeyi
  zamanlamaz.
- Kampanyalar ekranı (`components/CampaignList.vue`, DECISIONS 29): `GET /api/campaigns`
  listesini gösterir, 5 saniyede bir yeniler; satıra tıklamak `store.openCampaign(id)` ile aynı
  panoyu açar (`standalone`). Durum (`scheduled` / `sending` / `done` / `cancelled`) backend'de
  `campaign.stateOf` içinde türetilir; arayüzde yeniden hesaplama.
- Adres: `store.route` hesaplanır, `App.vue` hash ile iki yönlü eşitler; `store.navigate` hash'ten
  gelen adrese gider. Yeni bir ekran eklersen `lib/route.ts`, `route` ve `navigate` üçünü birlikte
  güncelle.
- Tasarım (DECISIONS 32): krem zemin, mürekkep rengi yazı, tek vurgu rengi; Inter Tight başlıklar,
  Instrument Serif italik ara metinler, gölge yerine çizgiler. Renkler `style.css` başındaki
  token'lardan gelir; krem varsayılan, koyu tema yalnızca `data-theme="dark"` ile (`lib/theme.ts`,
  localStorage). Sayfa düzeni: yapışkan üst menü, numaralı tıklanabilir adım çubuğu, her ekranda
  `.page-head` (bölüm etiketi + büyük başlık), adımın ana düğmeleri altta yapışkan `.actionbar`.
- Açılış sayfası (`components/HomePage.vue`): sabitlenmiş bir canvas sahnesi kaydırmayla oynar.
  Zaman çizelgesi `lib/story.ts` içinde saf fonksiyonlardır (`sceneAt`, `chapterPhase`); çizim de
  başlıklar da aynı ilerleme değerinden türetilir. Bir sahnenin ya da başlığın zamanını
  değiştirirsen `CHAPTERS` ile `sceneAt` aralıklarını birlikte değiştir; testler ikisinin
  örtüştüğünü kontrol eder. Sahnenin renkleri temadan bağımsızdır (`--film-bg`, `--film-fg`).
- Alıcı listesi (DECISIONS 33): panoda "Alıcıları gör" ile açılan yan panel. Durum store'da
  (`recipientsOpen`, `openRecipients`, `filterRecipients`, `loadRecipients(more)`); liste ancak
  panel açılınca yüklenir, yalnızca en son isteğin yanıtı kullanılır. Maskeleme backend'dedir.
  Süzgeçler (`RecipientFilter`: durum, kanal, dil, şehir, ad) sorgu parametresidir; durum
  düğmelerindeki sayılar yanıttaki `by_status`'tan gelir, panodaki toplamlardan değil. Kampanya
  listesinin süzgeci tarayıcıdadır (`lib/campaigns.ts`). Bastırılan alıcı satırında etiketle
  görünür (`Recipient.suppression`); kitle ekranı hariç kalan mesajları nedenine göre ayrı sayar
  (`audience.suppressed`, `audience.unsubscribed`).
- Onay ekranında "Hemen / Zamanla" seçimi; pano zamanlanmış kampanyada başlangıç saatini ve
  iptal butonunu gösterir. İptal iki tıklamadır ve sayfanın içinde sorulur (tarayıcı penceresi
  değil).
- Backend: `GET /api/campaigns/{id}/events` (SSE) ve `GET /metrics` (`internal/metrics`).
- Compose'ta `web` servisi (nginx, `:3000`). Yerel geliştirme: `cd frontend && npm install &&
  npm run dev` (Vite `/api`'yi `:8080`'e iletir).
- Tarayıcıda uçtan uca deneme mock LLM ile yapıldı; gerçek Claude ile deneme API üzerinden
  yapıldı (16 Kasım, DECISIONS 41).

## Teslim (aşama 6, tamam)

Bitti ölçütlerinin durumu:

| Ölçüt | Durum |
|---|---|
| Temiz bir clone'da `docker compose up --build` ve testler çalışıyor | ✅ 11 Kasım'da denendi: taze seed 381 / 222 / 254 / 58 verdi, kampanya gönderildi, backend ve frontend testleri geçti |
| CI yeşil | ✅ 12 Kasım: üç iş de (backend, frontend, compose) yeşil. İlk dört çalışma GitHub Actions arızası (11 Kasım 20:48 UTC) yüzünden kuyrukta kaldı; iptal edilebilir |
| `ai-sessions/` temiz | ✅ Dört dosya var ve tarandı. Üçüncü oturum masaüstü uygulamasının dışa aktarımından metne çevrildi ve iki dosyada durur: `…-03-…` 13 Kasım'daki export anına kadar, `2026-11-13-04-…` oradan 16 Kasım akşamına kadar. Araç çıktıları kısaltıldı; yerel kullanıcı adı, e-posta, ad soyad, API anahtarının adı ve API istek / çalışma alanı kimlikleri gizlendi; ekran görüntüleri alınmadı. Bağlam dolduğunda uygulamanın ürettiği özet export'a alınmaz, yerine tek satırlık not düşülür |
| Gerçek Claude denemesi yapıldı ya da yapılmadığı README'de yazıyor | ✅ 16 Kasım'da yapıldı (`claude-opus-5-5`): kural, akışlı içerik, çeviri ve gönderim. İki hata bulundu ve düzeltildi (DECISIONS 41); kapsamı ve denenmeyenler README "Bilinen eksikler"de |
| Gereksinim listesi madde madde kontrol edildi | ✅ `case.pdf` yeniden okundu; fonksiyonel, AI, kaynak ve teslim maddelerinin hepsi karşılanıyor. Bonuslardan yalnızca CI var |

Bütün ölçütler sağlandı; aşama 6 tamam. Teslimden önce bu oturumda çalışmaya devam edilirse
devam dosyası (`ai-sessions/2026-11-13-04-…`) yeniden alınır.

- CI üç iş çalıştırır: backend (gofmt, vet, `-race` ile testler, Postgres servisine karşı),
  frontend (`npm ci`, test, build), compose (README'deki kurulum + örnek brief'in kitle sayısı).
- Repo 11 Kasım'da herkese açık yapıldı.
