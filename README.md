# Smart Campaign Composer

Pazarlamacının doğal dille yazdığı kampanya isteğini Claude ile hedef kitleye ve SMS / email
mesajlarına çeviren, insan onayına sunan, kuyruk üzerinden sahte bir sağlayıcıya gönderen ve
gönderimi canlı izleten sistem.

> **Durum:** geliştirme sürüyor. Aşağıdaki "Durum" tablosu neyin hazır olduğunu gösterir.

## Kurulum

Gereken tek şey Docker (Compose v2.24+).

```bash
cp .env.example .env      # isteğe bağlı: ANTHROPIC_API_KEY ekle; boş kalırsa mock LLM kullanılır
docker compose up --build
```

Bu komut sırayla:

1. Postgres'i başlatır,
2. `init` servisiyle migration'ları uygular ve 5000 sahte kullanıcıyı yükler (zaten varsa atlar),
3. API'yi `http://localhost:8080` adresinde açar.

| Komut | Ne yapar |
|---|---|
| `make up` | Tüm sistemi kurar ve başlatır |
| `make test` | Tüm testleri container içinde, geçici bir Postgres'e karşı çalıştırır |
| `make reset` | Veritabanı volume'unu siler; sonraki `make up` sıfırdan seed eder |
| `make logs` | API loglarını izler |

`make` yoksa (ör. Windows): testler için
`docker compose --profile test run --rm --build test`.

## Endpoint'ler

| Yol | Açıklama |
|---|---|
| `GET /healthz` | Liveness: süreç ayakta mı |
| `GET /readyz` | Readiness: veritabanına ulaşılabiliyor mu (değilse 503) |

## Mimari

```
Vue (planlanan)
   │  HTTP + SSE
   ▼
Go API ──► Claude API / mock LLM
   │
   ▼
Postgres  ── users, events (seed)
          ── campaigns, messages (= kuyruk, planlanan)
   ▲
   │  SKIP LOCKED
Go worker (planlanan) ──► mock SMS / email provider (planlanan)
```

Kararların gerekçeleri [DECISIONS.md](DECISIONS.md) dosyasında.

### Klasörler

```
backend/
  cmd/api          HTTP sunucusu
  cmd/seed         migration + seed (tek seferlik)
  internal/config  ortam değişkenleri
  internal/db      bağlantı havuzu, gömülü migration'lar
  internal/seed    deterministik sahte kullanıcı üretimi
  internal/httpapi HTTP handler'ları
  internal/testdb  entegrasyon testleri için izole şema
frontend/          Vue uygulaması (planlanan)
ai-sessions/       Claude oturum export'ları
```

## Veri modeli

- **users**: ad, soyad, email, telefon, şehir, dil (`tr` / `en`), cinsiyet, doğum yılı,
  kayıt tarihi, kanal izinleri (`email_opt_in`, `sms_opt_in`).
- **events**: `cart_add` ve `purchase` olayları; kategori, tutar, zaman.

## Durum

| Alan | Durum |
|---|---|
| Docker compose, migration, seed, health | ✅ |
| Segment kuralı şeması, doğrulama, SQL derleyici | ⏳ |
| Claude entegrasyonu + mock LLM | ⏳ |
| Akışlı içerik üretimi, karakter limiti kontrolü | ⏳ |
| Kuyruk, worker, hız limiti, retry | ⏳ |
| Mock SMS / email provider | ⏳ |
| Vue arayüzü | ⏳ |
| Metrics endpoint'i | ⏳ |
| CI | ⏳ |
