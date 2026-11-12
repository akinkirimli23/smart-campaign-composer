# Smart Campaign Composer

Pazarlamacının doğal dille yazdığı kampanya isteğini Claude ile hedef kitleye ve SMS / email
mesajlarına çeviren, insan onayına sunan, kuyruk üzerinden sahte bir sağlayıcıya gönderen ve
gönderimi canlı izleten sistem. Gerçek kimseye mesaj gitmez: alıcılar üretilmiş sahte
kullanıcılardır, gönderim bu repodaki mock provider'a yapılır.

- Kararlar ve gerekçeleri: [DECISIONS.md](DECISIONS.md)
- Claude ile çalışma kayıtları: [ai-sessions/](ai-sessions/)

## Kurulum

Gereken tek şey Docker (Compose v2.24+).

```bash
docker compose up --build
```

Bu komut sırayla:

1. Postgres'i başlatır,
2. `init` servisiyle migration'ları uygular ve 5000 sahte kullanıcıyı yükler (zaten varsa atlar),
3. mock SMS / email provider'ı açar (`http://localhost:8090`),
4. API'yi ve içindeki gönderim worker'ını açar (`http://localhost:8080`),
5. arayüzü açar: **http://localhost:3000**

API key gerekmez; key yokken her şey mock LLM ile çalışır. Gerçek Claude ile denemek için:

```bash
cp .env.example .env
```

ve `.env` içinde `ANTHROPIC_API_KEY` değerini doldur (model `ANTHROPIC_MODEL` ile değişir,
varsayılan `claude-opus-5-5`).

| Komut | Ne yapar |
|---|---|
| `make up` | Tüm sistemi kurar ve başlatır |
| `make test` | Backend testlerini geçici bir Postgres'e karşı, arayüz testlerini vitest ile, container içinde çalıştırır |
| `make reset` | Veritabanı volume'unu siler; sonraki `make up` sıfırdan seed eder |
| `make logs` | API loglarını izler |

`make` yoksa (ör. Windows) testler için:

```bash
docker compose --profile test run --rm --build test
```

```bash
docker compose --profile test run --rm --build web-test
```

## Kullanım

`docker compose up --build` bittikten sonra arayüz **http://localhost:3000** adresindedir.

1. **Brief.** Kampanyayı kendi cümlelerinle yaz ("Örnek brief'i kullan" hazır bir metin koyar).
2. **Kitle.** Kuraldan türetilen açıklama, modelin yorumları ve kanal bazlı kişi sayıları.
   "Koşulları düzenle" bir form açar: koşullar listeden seçilerek değiştirilir, eklenir ya da
   kaldırılır; uygulandığında kural yeniden doğrulanır ve sayılar güncellenir.
3. **İçerik ve onay.** SMS ve email metinleri ekrana akar. Bittikten sonra düzenlenebilir;
   sayaçlar değişkenler doldurulmuş halini ölçer, yanda örnek bir alıcı için önizleme durur.
   Gönderim hemen yapılabilir ya da ileri bir tarihe zamanlanabilir. Onay sunucuda yeniden
   doğrular.
4. **Gönderim.** Kanal başına bekleyen / gönderilen / teslim edilen / başarısız / atlanan
   sayıları canlı güncellenir. Zamanlanmış kampanya başlayacağı saati gösterir; kampanya
   bitene kadar iptal edilebilir.

Mock LLM bir dil modeli değil, anahtar kelime eşleştiricidir: şehir, dil, cinsiyet, yaş, üyelik
tarihi, sepet, satın alma, harcama ve sipariş sayısı ifadelerini tanır; içerik için brief'teki
indirim oranını sabit bir metne yerleştirir. Çıktısı gerçek modelinkiyle aynı doğrulamadan
geçer. Brief'e `#demo-retry` eklenirse içerik adımında ilk deneme bilerek reddedilir ve yeniden
deneme ekranda görülür.

| Adres | Ne |
|---|---|
| http://localhost:3000 | Arayüz (nginx; `/api` isteklerini API'ye iletir) |
| http://localhost:8080 | API ve `/metrics` |
| http://localhost:8090 | Mock SMS / email provider |
| http://localhost:3000/?debug | Arayüz, kitle adımında kuralın JSON hali de görünür (geliştirici görünümü) |

## Endpoint'ler

| Yol | Açıklama |
|---|---|
| `GET /healthz` | Liveness: süreç ayakta mı |
| `GET /readyz` | Readiness: veritabanına ulaşılabiliyor mu (değilse 503) |
| `POST /api/segments/generate` | `{"brief": "..."}` → LLM'in önerdiği kural, kuraldan türetilen açıklama, LLM'in yorumları ve kanal bazlı kitle sayısı. Format brief'i ifade edemiyorsa `status: "unsupported"` |
| `POST /api/segments/count` | `{"rule": {...}}` → elle düzenlenmiş bir kuralı aynı doğrulayıcıdan geçirir ve yeniden sayar; geçersizse 422 ve `{path, message}` listesi |
| `GET /api/segments/options` | Kural düzenleyicisi için izinli değerler (şehirler, kategoriler, olaylar, metrikler) ve sınırlar; doğrulayıcının kendi tablolarından |
| `POST /api/content/generate` | `{"brief": "..."}` → SMS ve email içeriği, SSE akışı olarak. Olaylar: `attempt`, `delta` (alan bazlı metin), `rejected` (doğrulama hataları), `result` (doğrulanmış içerik, ölçülen uzunluklar, limitler), `error`. İstek hataları (boş brief vb.) normal JSON hata yanıtıdır |
| `POST /api/campaigns` | Onay: `{"brief", "rule", "content", "send_at"?}`. `send_at` (RFC 3339, en fazla 30 gün sonrası) verilirse gönderim o saatte başlar. Kural ve içerik (elle düzenlenmiş olsa da) yeniden doğrulanır; geçerliyse kampanya ve alıcıları kuyruğa girer. `201` ve kanal başına kuyruğa giren mesaj sayısı; geçersizse `422` ve `{path, message}` listesi |
| `GET /api/campaigns/{id}` | Gönderim durumu: kanal başına `pending` / `sending` / `sent` / `failed` / `skipped` / `cancelled` sayıları, `send_at`, `cancelled_at` ve `done` |
| `POST /api/campaigns/{id}/cancel` | Henüz gönderilmemiş mesajları iptal eder; gönderilenler kalır. Tekrarlanabilir |
| `GET /api/campaigns/{id}/events` | Canlı gönderim durumu, SSE: `status` (sayılar değiştikçe) ve bitince `done` |
| `GET /metrics` | Prometheus metin formatı: `scc_campaigns`, `scc_messages{channel,status}`, `scc_provider_requests_total{channel,outcome}` |

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
Tarayıcı (Vue 3 + Pinia)
   │  HTTP + SSE, tek origin
   ▼
nginx ──► statik arayüz
   │  /api
   ▼
Go API ─────────────► Claude API  (key yoksa mock LLM)
   │  ▲
   │  └ gönderim worker'ı (aynı süreç) ──► mock SMS / email provider
   ▼
Postgres
   users, events          sahte alıcılar ve davranışları (seed)
   campaigns              onaylanan kural ve içerik
   messages               alıcı × kanal; bu tablo kuyruktur
```

**Bir kampanyanın yolu**

1. **Kitle.** Claude brief'ten yapılandırılmış bir kural (JSON) üretir. Backend kuralı kapalı
   değer kümelerine karşı doğrular ve parametreli SQL'e kendisi derler; modelin yazdığı hiçbir
   metin SQL'e girmez. Pazarlamacının okuduğu açıklama modelden değil kuraldan türetilir.
   Kural formatı brief'i ifade edemiyorsa model "desteklenmiyor" der; eksik koşullu kural
   üretilmez.
2. **İçerik.** Claude SMS ve email şablonlarını yazar; metin ekrana akar. Limitler (SMS 160,
   konu 80) değişkenler en uzun değerleriyle doldurulmuş gibi ölçülür.
3. **Güvenilirlik.** Her iki adımda da doğrulamadan geçmeyen çıktı, hatalarıyla birlikte modele
   geri verilir; en fazla üç deneme. Yine olmazsa kullanıcıya hata gösterilir, hiçbir şey
   çalıştırılmaz.
4. **Onay.** Pazarlamacı metni düzenleyebilir. Onayda kural ve metinler sunucuda yeniden
   doğrulanır, kampanya ve alıcıları tek transaction'da kuyruğa girer.
5. **Gönderim.** Worker satırları `FOR UPDATE SKIP LOCKED` ile kiralar, kanal başına saniyede en
   fazla 100 mesaj yollar, hatada üstel bekleme ile yeniden dener, 429'da `Retry-After` kadar
   kanalı durdurur. Veritabanı kısıtı ve idempotency key aynı kişiye iki kez gitmesini önler.
6. **Takip.** Pano durumu SSE ile canlı alır; `/metrics` aynı satırları sayar.

Testler API key istemez: LLM bir arayüzün arkasındadır ve testlerde sahte bir model kullanılır.

### Klasörler

```
backend/
  cmd/api          HTTP sunucusu
  cmd/seed         migration + seed (tek seferlik)
  cmd/mockprovider sahte SMS / email provider
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
  internal/metrics sayaçlar ve Prometheus metin çıktısı
  internal/llm     LLM arayüzü; llm/anthropicllm Claude istemcisi, llm/mockllm key yokken kullanılan mock
  internal/httpapi HTTP handler'ları
  internal/testdb  entegrasyon testleri için izole şema
frontend/          Vue 3 + TypeScript + Pinia arayüzü (nginx ile sunulur)
  src/stores       tüm akışın durumu (Pinia)
  src/api          API istemcisi ve tipleri
  src/lib          SSE ayrıştırıcı, şablon ölçümü (canlı sayaç), kural düzenleyicisinin mantığı
  src/components   adım ekranları
.github/workflows/ CI
ai-sessions/       Claude oturum export'ları
```

## Veri modeli

- **users**: ad, soyad, email, telefon, şehir, dil (`tr` / `en`), cinsiyet, doğum yılı,
  kayıt tarihi, kanal izinleri (`email_opt_in`, `sms_opt_in`).
- **events**: `cart_add` ve `purchase` olayları; kategori, tutar, zaman.
- **campaigns**: brief, onaylanan kural ve içerik (JSON), kitlenin hesaplandığı an, gönderimin
  başlayacağı an, iptal edildiyse iptal anı.
- **messages**: kampanya × kullanıcı × kanal; durum (`pending`, `sending`, `sent`, `failed`,
  `skipped`, `cancelled`), deneme sayısı, bir sonraki deneme zamanı. `UNIQUE (campaign_id, user_id, channel)`.

Seed deterministiktir: aynı `SEED_USERS` / `SEED_RANDOM` her makinede aynı veriyi üretir. Olay
zamanları seed'in yüklendiği ana göredir.

## Testler

| Ne | Nerede |
|---|---|
| Kural doğrulama, SQL derleme, enjeksiyon denemeleri | `backend/internal/segment` |
| Derlenen SQL'in Go'da yazılmış referans hesapla karşılaştırılması; 381 / 222 / 254 / 58 | `backend/internal/audience` |
| İçerik limitleri, şablonlar, akış çözücü, yeniden deneme (bozuk JSON, uzun SMS, bilinmeyen değişken) | `backend/internal/content` |
| Aynı kişiye iki kez gitmeme (iki worker, hata veren provider), hız limiti, retry, `Retry-After`; zamanı gelmeden gönderilmeme, iptalden sonra hiçbir şeyin gitmemesi | `backend/internal/delivery` |
| Claude istemcisi (yerel bir SSE sunucusuna karşı, key olmadan) | `backend/internal/llm/anthropicllm` |
| HTTP uçları, SSE akışları, onayda yeniden doğrulama | `backend/internal/httpapi` |
| Arayüz: SSE ayrıştırıcı, canlı sayaç ölçümü, kural düzenleyicisinin mantığı, akışın durumu | `frontend/src/**/*.test.ts` |

CI (GitHub Actions) her push'ta üç iş çalıştırır: backend (gofmt, vet, yarış dedektörüyle
testler, Postgres'e karşı), frontend (tip kontrolü, testler, derleme) ve `docker compose up` ile
kurulumun kendisi.

## Bilinen eksikler

- **Gerçek Claude ile uçtan uca deneme yapılmadı.** Geliştirme boyunca mock LLM kullanıldı.
  İstemci (resmi Go SDK, akış, structured outputs) yerel bir sunucuya karşı test edildi, ama
  gerçek API'nin şemaları kabul ettiği ve modelin ürettiği çıktının kalitesi doğrulanmadı.
- Arayüz masaüstü genişliğinde ve koyu temada denendi; dar ekran ve açık tema denenmedi.
- Ekran bileşenleri için otomatik test yok; mantık ve durum birim testleriyle, ekranlar tarayıcıda
  elle denendi.
- Sayfa yenilenince akış baştan başlar; devam eden bir kampanyanın panosuna dönülemez.
- Provider sürekli 429 dönerse mesaj süresiz bekler; toplam bekleme için üst sınır yok
  (DECISIONS 21).
- LLM çağrıları için metrik yok, yalnızca log (DECISIONS 23).

## Segment kuralı neleri ifade edebilir

| İstek | Örnek |
|---|---|
| Şehir, dil, cinsiyet (dahil / hariç), yaş aralığı | "Ankara ve İzmir'deki 25-34 yaş kadınlar" |
| Sepete ekleme / satın alma: yaptı ya da yapmadı, son N günde, isteğe bağlı kategori | "Son 7 günde elektronikte sepete ekleyenler", "60 gündür alışveriş yapmayanlar" |
| Olay sayısı | "Son 30 günde en az 3 kez sepete ekleyenler" |
| Üyelik tarihi | "Son 7 günde kayıt olanlar", "1 yıldan eski üyeler" |
| Toplam harcama, sipariş sayısı (tüm zamanlar ya da son N gün) | "1000 TL üzeri harcayanlar", "en az 5 sipariş verenler" |
| Koşulların hepsi (VE) ya da herhangi biri (VEYA), tek seviye | |

Formatın ifade edemediği bir istekte kural üretilmez; arayüz neyin desteklenmediğini gösterir.
Eksik koşullu bir kural daha geniş bir kitle demek olurdu.

## Bonuslar

| Bonus | Durum |
|---|---|
| CI pipeline | Yapıldı (GitHub Actions: backend, frontend, `docker compose up`) |
| Zamanlanmış gönderim | Yapıldı; iptal edilebilir (DECISIONS 27) |
| Kullanıcının diline göre içerik | Yapılmadı (DECISIONS 17) |
| A/B varyantları | Yapılmadı. A/B'nin anlamı kazananı seçmektir; bunun için açılma ya da tıklama verisi gerekir ve mock provider'da yok. Uydurma veriyle yapmak yerine dışarıda bırakıldı |

## Sonraya bırakılanlar

Zaman kalırsa ele alınacaklar ve bilinçli olarak yapılmayanlar.

| Konu | Not |
|---|---|
| Tüm kullanıcılara gönderim ("herkese gönder") | Koşulsuz kural bilerek yasak: modelin koşulları unutup herkese gönderim yapmasını önler. Açık bir "tüm kullanıcılar" seçeneği sonradan eklenebilir |
| Segment kuralında iç içe VE / VEYA grupları | Bilinçli olarak yok (DECISIONS 13) |
| Kesin "büyüktür", "en fazla N kez", kategori bazında harcama | Karşılaştırmalar kapsayıcı; ayrıntılar DECISIONS 25 |
| Ülke, sadakat seviyesi, cihaz; ürün görüntüleme, uygulama / email açma | Seed verisinde yok |
| Gerçek SMS kodlaması (Türkçe karakterde 70 karakterlik parça) | Limit case'teki gibi düz karakter sayısı (DECISIONS 19) |
| Zamanlanmış kampanyayı düzenleme, alıcının yerel saatinde gönderim | Zamanlanan kampanya yalnızca iptal edilebilir; saat pazarlamacının saat dilimindedir (DECISIONS 27) |
| Gönderilen metnin saklanması | Şablon ve kullanıcıdan yeniden üretilebilir; satırda tutulmuyor (DECISIONS 22) |
| Kullanıcının diline göre içerik (bonus) | Kampanya başına tek içerik seti var; `en` kullanıcılar da aynı metni alır (DECISIONS 17) |
