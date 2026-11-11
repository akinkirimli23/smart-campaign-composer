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
| `POST /api/segments/generate` | `{"brief": "..."}` → LLM'in önerdiği kural, kuraldan türetilen açıklama, LLM'in yorumları ve kanal bazlı kitle sayısı. Format brief'i ifade edemiyorsa `status: "unsupported"` |
| `POST /api/segments/count` | `{"rule": {...}}` → elle düzenlenmiş bir kuralı aynı doğrulayıcıdan geçirir ve yeniden sayar; geçersizse 422 ve `{path, message}` listesi |

```bash
curl -s localhost:8080/api/segments/generate -H 'Content-Type: application/json' \
  -d '{"brief":"Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul bölgesindeki kullanıcılara %30 indirim hatırlatması gönder."}'
# "audience": {"total": 381, "sms": 222, "email": 254, "unreachable": 58}
```

Kitle sayısı sorgu anına göre hesaplanır; seed verisi ise yüklendiği ana göre üretilir. Seed
eskidikçe (olaylar 30 günlük pencereden çıktıkça) sayılar düşer; `make reset` ve yeniden
`make up` veriyi tazeler.

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
  internal/segment segment kuralı: doğrulama, SQL derleme, açıklama, LLM ile üretim
  internal/audience kanal bazlı kitle sayımı
  internal/llm     LLM arayüzü; llm/anthropicllm Claude istemcisi, llm/mockllm key yokken kullanılan mock
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
| Segment kuralı şeması, doğrulama, SQL derleyici, kitle sayısı endpoint'i | ✅ |
| Mock LLM, hatalı çıktıda geri besleme ile yeniden deneme (segment) | ✅ |
| Gerçek Claude istemcisi (resmi Go SDK, akışlı; gerçek key ile henüz denenmedi) | ✅ |
| Akışlı içerik üretimi, karakter limiti kontrolü | ⏳ |
| Kuyruk, worker, hız limiti, retry | ⏳ |
| Mock SMS / email provider | ⏳ |
| Vue arayüzü | ⏳ |
| Metrics endpoint'i | ⏳ |
| CI | ⏳ |
