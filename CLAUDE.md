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
  eski kaydı silme, nedenini yaz.
- `README.md`: kurulum ve "Durum" tablosu. Bir parça bitince tabloyu güncelle.

## Komutlar

```bash
docker compose up --build        # Postgres + init (migration + seed) + api, http://localhost:8080
make test                        # tüm testler container içinde, geçici Postgres'e karşı
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
2. **Şehir ve kategori kapalı küme.** Geçerli değerler `seed.Cities()` ve `seed.Categories`.
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
   segment derleyicisinin testleri bu sayıyı referans alabilir.

## Kod düzeni

- Go 1.24, standart `net/http` (Go 1.22 route pattern'leri), `pgx/v5`, `log/slog`.
  Gereksiz bağımlılık ekleme; ekleyeceksen DECISIONS'a nedenini yaz.
- `backend/cmd/*` giriş noktaları, `backend/internal/*` paketler.
- Migration'lar `backend/internal/db/migrations/NNN_ad.sql`, binary'ye gömülü, sıralı.
  Uygulanmış migration'ı düzenleme, yenisini ekle.
- DB gerektiren testler `testdb.New(t)` kullanır (her test için ayrı şema).
- Kod yorumları İngilizce, dokümanlar (README, DECISIONS) Türkçe.
- `.env` asla commit edilmez. `ai-sessions/` içine koymadan önce API key ve kişisel bilgileri temizle.

## Durum ve plan

İş gün bazlı değil, aşama bazlı ilerler. Bir aşamanın "bitti" ölçütlerinin hepsi sağlanınca
sonrakine geçilir.

| # | Aşama | Kapsam | Bitti ölçütü | Durum |
|---|---|---|---|---|
| 1 | İskelet | compose, şema (`users`, `events`), deterministik seed, `/healthz`, `/readyz` | Compose temiz kurulumda kalkıyor, testler geçiyor, örnek brief SQL ile 381 / 222 / 254 / 58 veriyor | ✅ |
| 2 | Kitle | Segment kuralı JSON şeması, doğrulayıcı, parametreli SQL derleyici (**zaman parametre olarak verilir**, SQL'de `now()` yok), kanal bazlı kitle sayısı endpoint'i, mock LLM | Örnek brief sabit bir tarihte 381 / 222 / 254 / 58 veriyor; geçersiz şehir, bilinmeyen alan ve SQL enjeksiyonu denemesi testle reddediliyor | ⏳ |
| 3 | İçerik | Akışlı (SSE) SMS / email üretimi, değişkenler doldurulduktan sonra karakter limiti kontrolü, hatalı çıktıda hatayı Claude'a geri verip sınırlı sayıda yeniden deneme | Mock LLM'in bozuk JSON, limiti aşan SMS ve bilinmeyen değişken senaryoları testte doğru ele alınıyor | ⏳ |
| 4 | Gönderim | `campaigns` / `messages` tabloları, worker, kanal başına 100/sn, retry + backoff, 429'da `Retry-After`'a uyma; hata, gecikme ve 429 döndüren mock provider | Testte aynı kişiye iki kez mesaj gitmiyor ve hız limiti aşılmıyor | ⏳ |
| 5 | Frontend | Vue 3 + TS + Pinia: brief → kitle → içerik → onay → canlı pano akışı; metrics endpoint'i | Mock LLM ile tarayıcıda brief'ten canlı panoya uçtan uca bir kampanya gönderilebiliyor; panodaki sayılar metrics endpoint'iyle tutarlı; pazarlamacının elle düzenlediği metin onay anında backend'de yeniden doğrulanıyor (SMS 160, konu 80, yalnızca izinli değişkenler); arayüz karakter sayısını canlı gösteriyor | ⏳ |
| 6 | Teslim | README, DECISIONS, CI (GitHub Actions), `ai-sessions/`, temiz kurulum denemesi, "Case gereksinimleri" listesinin madde madde kontrolü | Temiz bir clone'da README'deki adımlarla `docker compose up --build` ve testler çalışıyor; CI yeşil; `ai-sessions/` key ve kişisel bilgiden temizlenmiş; gerçek Claude ile en az bir uçtan uca deneme yapıldı ya da yapılmadığı README'de yazıyor; "Case gereksinimleri" listesindeki her madde işaretli ya da README'de eksik olarak belirtilmiş | ⏳ |

### Sınırlar

- Aşama 1–4 en geç **15 Kasım akşamı** biter.
- **17 Kasım'da yeni özellik yok**; yalnızca aşama 6.
- Bir aşama uzarsa kapsamı daraltılır, kesilen kısım README'ye yazılır.

### Çalışma kuralları

- **Aşama sonu:** testler çalıştırılır, commit atılır, push'lanır ve kullanıcıya kısa bir özet verilir.
- **Karar kuralı:** DECISIONS'a girecek bir karar verilmeden önce kullanıcıya iki seçenek,
  artısı ve eksisiyle gösterilir; kullanıcı seçince devam edilir.

## Yeni oturum başlangıcı

1. `make test` çalıştır (make yoksa `docker compose --profile test run --rm --build test`).
2. Yukarıdaki durum tablosuna bak; ilk ⏳ aşama sıradakidir.
3. O aşamanın bitti ölçütlerinden eksik olanlarla devam et.

## Case gereksinimleri

`case.pdf`'ten çıkarılmış kontrol listesi. Sütun: karşılanacağı aşama. ✅ = karşılandı.
Aşama 6'da teslimden önce madde madde kontrol edilir; karşılanmayan README'de belirtilir.

### Fonksiyonel

| Gereksinim | Aşama |
|---|---|
| Kullanıcı akışı 1: Claude brief'ten segment kuralı üretir, arayüz kaç kişiye gideceğini gösterir | 2, 5 |
| Kullanıcı akışı 2: SMS ve email içeriği ekrana stream edilerek akar | 3, 5 |
| Kullanıcı akışı 3: pazarlamacı önizler, gerekirse düzenler ve onaylar | 5 |
| Kullanıcı akışı 4: mesajlar kuyruk üzerinden mock provider'a gönderilir | 4 |
| Kullanıcı akışı 5: dashboard gönderim durumunu canlı gösterir | 5 |
| Gerçek kimseye mesaj gitmez: alıcılar sahte kullanıcılar, gönderim mock provider'a | 1 ✅ (seed), 4 |
| Kitle: Claude'un segment kuralı backend'de doğrulanır, LLM çıktısı doğrudan SQL olarak çalıştırılmaz | 2 |
| Kitle: izni olmayan kullanıcıya o kanaldan mesaj gönderilmez | 2 (sayım), 4 (gönderim) |
| İçerik: SMS en fazla 160 karakter | 3, 5 (elle düzenleme) |
| İçerik: email konusu en fazla 80 karakter | 3, 5 (elle düzenleme) |
| İçerik: `{{first_name}}` gibi kişiselleştirme değişkenleri desteklenir | 3, 4 (gönderimde doldurma) |
| Gönderim: asenkron kuyruk | 4 |
| Gönderim: kanal başına saniyede en fazla 100 mesaj | 4 |
| Gönderim: aynı kişiye aynı mesaj iki kez gitmez | 4 |
| Gönderim: retry stratejisi tasarlanır (ve DECISIONS'ta gerekçelendirilir) | 4 |
| Takip: canlı gönderim durumu endpoint'i | 5 |
| Takip: metrics endpoint'i | 5 |
| Takip: health check endpoint'i | 1 ✅ |

### AI

| Gereksinim | Aşama |
|---|---|
| Claude'un çıktısı yapılandırılmış JSON, backend'de kontrol ediliyor (segment kuralı) | 2 |
| Claude'un çıktısı yapılandırılmış JSON, backend'de kontrol ediliyor (içerik) | 3 |
| "Çağırmak yeterli değil, güvenilir hale getir": hatalı çıktıda hata geri beslenip sınırlı yeniden deneme | 3 |
| Testler gerçek API key olmadan, mock bir LLM ile çalışıyor | 2, 3 (her aşamada korunur) |

### Kaynaklar

| Gereksinim | Aşama |
|---|---|
| API key `.env`'deki `ANTHROPIC_API_KEY`'den okunuyor | 1 ✅ (config), 2–3 (kullanım) |
| Repoda `.env.example` var | 1 ✅ |
| Birkaç bin kullanıcılık seed (ad, email, telefon, şehir, alışveriş geçmişi, kanal izinleri) | 1 ✅ |
| Mock SMS / email provider gerçek bir servisi taklit ediyor | 4 |
| Mock provider zaman zaman hata, gecikme ve 429 döndürüyor | 4 |
| Frontend Vue.js, backend Go | 1 ✅ (Go), 5 (Vue) |

### Teslim edilecekler

| Gereksinim | Aşama |
|---|---|
| GitHub repository | 1 ✅ |
| `docker compose up` ile tek komutla kurulum, seed verisi dahil | 1 ✅ |
| `docker compose up` ile tek komutla kurulum, mock provider dahil | 4 |
| `make test` ile çalışan testler | 1 ✅ (her aşamada korunur) |
| `README.md`: kurulum ve kısa mimari özeti | 6 |
| `DECISIONS.md`: önemli kararlar ve nedenleri | her aşama, 6 |
| `ai-sessions/`: tüm Claude session export'ları, API key ve kişisel bilgiden temizlenmiş | her aşama, 6 |
| Yetiştirilemeyenler README'de belirtilmiş | 6 |

### Bonus (isteğe bağlı)

| Gereksinim | Aşama |
|---|---|
| Zamanlanmış gönderim | planda yok |
| A/B varyantları | planda yok |
| Kullanıcının diline göre içerik | planda yok (seed'de %10 `en` hazır) |
| CI pipeline | 6 |

## 12 Kasım için başlangıç önerisi: segment kuralı

Kesinleşmiş değil, tartışılacak taslak:

```json
{
  "match": "all",
  "conditions": [
    { "type": "attribute", "field": "city", "op": "in", "values": ["İstanbul"] },
    { "type": "event", "event": "cart_add", "within_days": 30, "exists": true },
    { "type": "event", "event": "purchase", "within_days": 30, "exists": false }
  ],
  "explanation": "İstanbul'da son 30 günde sepete ekleyip satın almayanlar"
}
```

- `attribute` alanları: `city`, `language`, `gender`, `birth_year` (her biri için izinli op listesi).
- `event` koşulu: `event` ∈ {`cart_add`, `purchase`}, opsiyonel `category`, `within_days` 1–365,
  `exists` true/false → `EXISTS` / `NOT EXISTS` alt sorgusu.
- Sınırlar: en fazla N koşul, iç içe grup yok (ya da tek seviye), bilinmeyen alan reddedilir
  (`DisallowUnknownFields`).
- Doğrulama hataları insan okunur ve Claude'a geri verilebilir olmalı
  (ör. `conditions[0].values[0]: "Istanbul" geçerli bir şehir değil; geçerli: İstanbul, Ankara, …`).
- Kitle sayısı endpoint'i kanal bazında döner: toplam, SMS'e uygun, email'e uygun, ulaşılamaz.
