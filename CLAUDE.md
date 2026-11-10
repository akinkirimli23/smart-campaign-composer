# CLAUDE.md

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

| Gün | İş | Durum |
|---|---|---|
| 11 Kas | İskelet: compose, şema (`users`, `events`), seed, `/healthz`, `/readyz` | ✅ (11 Kas: compose Windows + Docker 28 üzerinde kalktı, testler geçti, 381/222/254/58 SQL ile doğrulandı) |
| 12 Kas | Segment kuralı: JSON şeması, doğrulayıcı, SQL derleyici, kitle sayısı endpoint'i, mock LLM | ⏳ |
| 13 Kas | İçerik: akışlı (SSE) SMS / email üretimi, limit ve değişken doğrulaması, hatalı çıktıda geri besleme ile yeniden deneme | ⏳ |
| 14 Kas | Gönderim: `campaigns` / `messages` tabloları, worker, kanal başına 100/sn, retry + backoff, 429'da `Retry-After`; mock provider (hata, gecikme, 429) | ⏳ |
| 15–16 Kas | Vue 3 + TS + Pinia: brief → kitle → içerik → onay → canlı pano; metrics endpoint'i; gerçek Claude ile deneme | ⏳ |
| 17 Kas | README, DECISIONS, CI (GitHub Actions), `ai-sessions/`, temiz makinede kurulum denemesi. Yeni özellik yok. | ⏳ |

## İlk oturumda yapılacaklar

1. Kökteki `Makefile.txt` dosyasının adı `Makefile` olmalı (önceki ortam bu dosyayı yazamadı).
2. `docker compose up --build` çalıştır, `curl localhost:8080/readyz` ile doğrula. Sorun varsa düzelt.
3. `make test` çalıştır; seed'in `TestLoadIsIdempotent` testinin atlanmadan geçtiğini kontrol et.
4. Mevcut kodu (özellikle `internal/seed`, `internal/db`) kullanıcıya anlat; kullanıcı her satırı
   review görüşmesinde savunabilmeli.
5. `git init`, ilk commit, GitHub'a push.

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
