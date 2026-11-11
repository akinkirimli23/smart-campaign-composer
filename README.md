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
| `POST /api/content/generate` | `{"brief": "..."}` → SMS ve email içeriği, SSE akışı olarak. Olaylar: `attempt`, `delta` (alan bazlı metin), `rejected` (doğrulama hataları), `result` (doğrulanmış içerik, ölçülen uzunluklar, limitler), `error`. İstek hataları (boş brief vb.) normal JSON hata yanıtıdır |
| `POST /api/campaigns` | Onay: `{"brief", "rule", "content"}`. Kural ve içerik (elle düzenlenmiş olsa da) yeniden doğrulanır; geçerliyse kampanya ve alıcıları kuyruğa girer. `201` ve kanal başına kuyruğa giren mesaj sayısı; geçersizse `422` ve `{path, message}` listesi |
| `GET /api/campaigns/{id}` | Gönderim durumu: kanal başına `pending` / `sending` / `sent` / `failed` / `skipped` sayıları ve `done` |

```bash
curl -s localhost:8080/api/segments/generate -H 'Content-Type: application/json' \
  -d '{"brief":"Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul bölgesindeki kullanıcılara %30 indirim hatırlatması gönder."}'
# "audience": {"total": 381, "sms": 222, "email": 254, "unreachable": 58}
```

Kitle sayısı sorgu anına göre hesaplanır; seed verisi ise yüklendiği ana göre üretilir. Seed
eskidikçe (olaylar 30 günlük pencereden çıktıkça) sayılar düşer; `make reset` ve yeniden
`make up` veriyi tazeler.

İçerik akışını denemek için (`-N` tamponlamayı kapatır):

```bash
curl -sN localhost:8080/api/content/generate -H 'Content-Type: application/json' \
  -d '{"brief":"Sepetine ürün ekleyip almayanlara %30 indirim hatırlatması gönder. #demo-retry"}'
```

Brief'teki `#demo-retry` yalnızca mock LLM'e özeldir: ilk yanıtı bilerek geçersiz üretir
(bilinmeyen değişken), böylece "reddet, hatayı geri ver, yeniden dene" döngüsü API key olmadan
izlenebilir.

Limitler değişkenler doldurulduktan sonra ölçülür: `{{first_name}}`, `{{last_name}}` ve
`{{city}}` veritabanındaki en uzun değerleriyle sayılır (DECISIONS 19).

### Mock provider

`docker compose up` ile birlikte kalkar (`http://localhost:8090`). Gerçek kimseye mesaj gitmez.

| Yol | Açıklama |
|---|---|
| `POST /v1/sms/messages`, `POST /v1/email/messages` | Mesaj kabul eder. `Idempotency-Key` başlığı zorunlu; aynı anahtar ikinci kez gelirse ilk mesajın id'sini döner, yeniden teslim etmez |
| `GET /v1/messages?limit=N` | Son kabul edilen mesajlar (doldurulmuş metinle) |
| `GET /stats` | Kanal başına sayaçlar: `accepted`, `duplicates`, `rate_limited`, `errors`, `lost_replies` |

Varsayılan olarak isteklerin %5'ine 5xx, %1'ine 429 (`Retry-After` ile) döner, %1'inde mesajı
kabul edip yanıtı "kaybeder" (504) ve her isteği 20–250 ms bekletir. Oranlar `.env` içindeki
`MOCK_*` değişkenleriyle değişir.

Bir kampanya bittiğinde `GET /stats` içindeki `accepted`, `GET /api/campaigns/{id}` içindeki
`sent` ile aynı olmalıdır: her alıcıya tam bir kez.

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
  internal/campaign onaylanan kampanya ve kuyruğa alma
  internal/delivery worker: kiralama, hız limiti, retry, provider istemcisi
  internal/mockprovider sahte SMS / email provider (cmd/mockprovider)
  internal/content içerik şablonları: doğrulama, limit ölçümü, akışlı üretim
  internal/validation ortak doğrulama hatası biçimi ({path, message} listesi)
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
| Akışlı (SSE) içerik üretimi, kişiselleştirme sonrası karakter limiti, hatada geri besleme ile yeniden deneme | ✅ |
| Kuyruk (Postgres), worker, kanal başına 100/sn, retry + backoff, 429'da `Retry-After` | ✅ |
| Mock SMS / email provider (hata, gecikme, 429, kaybolan yanıt) | ✅ |
| Vue arayüzü | ⏳ |
| Metrics endpoint'i | ⏳ |
| CI | ⏳ |

## Yapılmayanlar

| Konu | Not |
|---|---|
| Segment kuralında üyelik tarihi, olay sayısı ("en az 3 kez"), toplam harcama, sipariş sayısı | Veri var, kural formatında yok. Böyle bir brief "desteklenmiyor" yanıtı alır; yanlış kitle üretilmez (DECISIONS 20) |
| Segment kuralında iç içe VE / VEYA grupları | Bilinçli olarak yok (DECISIONS 13) |
| Ülke, sadakat seviyesi, cihaz; ürün görüntüleme, uygulama / email açma | Seed verisinde yok |
| Gerçek SMS kodlaması (Türkçe karakterde 70 karakterlik parça) | Limit case'teki gibi düz karakter sayısı (DECISIONS 19) |
| Gönderilen metnin saklanması | Şablon ve kullanıcıdan yeniden üretilebilir; satırda tutulmuyor (DECISIONS 22) |
| Kullanıcının diline göre içerik (bonus) | Yapılmadı. Kampanya başına tek içerik seti var; `en` kullanıcılar da aynı metni alır (DECISIONS 17) |
