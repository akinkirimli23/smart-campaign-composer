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

İlk kurulum, Go, Node ve Postgres imajları indirildiği için birkaç dakika sürebilir; sonraki
açılışlar bir iki dakikadır. Şu dört port boş olmalı; biri doluysa o servis açılmaz:

| Port | Servis |
|---|---|
| 3000 | Arayüz |
| 8080 | API |
| 8090 | Mock SMS / email provider |
| 5433 | Postgres (yerel bir Postgres ile çakışmasın diye 5432 değil) |

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

Adres bir **açılış sayfası** ile açılır: kaydırdıkça ilerleyen bir sahne ürünün ne yaptığını
anlatır (brief → kitle → mesaj → gönderim). "Kampanya oluştur" dört adımlı akışa
(`#/olustur`), "Kampanyalar" listeye götürür. Adım çubuğundan tamamlanmış bir adıma geri
dönülebilir; her adımın ana düğmeleri ekranın altında sabit durur.

1. **Brief.** Kampanyayı kendi cümlelerinle yaz ya da hazır örneklerden birini seç.
2. **Kitle.** Kuraldan türetilen açıklama, modelin yorumları ve kanal bazlı kişi sayıları.
   "Koşulları düzenle" bir form açar: koşullar listeden seçilerek değiştirilir, eklenir ya da
   kaldırılır; uygulandığında kural yeniden doğrulanır ve sayılar güncellenir. Yanda kitlenin
   dil ve sadakat seviyesi dağılımı görünür. Brief açıkça herkesi istiyorsa ("tüm kullanıcılara
   gönder") kural koşulsuzdur ve ekran bunu uyarıyla söyler; aynı seçenek formun başında da var.
3. **İçerik ve onay.** SMS ve email metinleri ekrana akar. Bittikten sonra düzenlenebilir;
   sayaçlar değişkenler doldurulmuş halini ölçer, yanda örnek bir alıcı için önizleme durur.
   Gönderim hemen yapılabilir ya da ileri bir tarihe zamanlanabilir; brief'te bir zaman
   geçiyorsa ("cuma sabahı gönder") alan onunla dolu gelir ve değiştirilebilir. Onay sunucuda
   yeniden doğrular.
   Kitle birden fazla dil okuyorsa (seed'de Türkçe ve İngilizce) metin önce en çok kişinin
   okuduğu dilde yazılır. "İngilizce metnini yaz" diğer dilleri, o anki metinden, tek istekte
   ekler; diller sekmelerde ayrı ayrı düzenlenir ve her alıcı yalnızca kendi dilindeki metni
   alır. Kitlenin bir dili için metin yokken onay kapalıdır; kampanya tek bir dilin okurlarına
   gidecekse kitle adımında kurala dil koşulu eklenir.
4. **Gönderim.** Kanal başına bekleyen / gönderilen / teslim edilen / başarısız / atlanan
   sayıları canlı güncellenir. Zamanlanmış kampanya başlayacağı saati gösterir; kampanya
   bitene kadar iptal edilebilir.

Sağ üstteki **Kampanyalar** tüm kampanyaları listeler: zamanlanmış, gönderilen, tamamlanmış
ve iptal edilmiş olanlar. Bir satıra tıklayınca o kampanyanın panosu açılır; zamanlanmış bir
kampanya oradan iptal edilir. Panodaki "Alıcıları gör" butonu, mesajların kimlere gittiğini ve gitmeyenlerin nedenini
gösteren bir panel açar (adresler maskeli); liste duruma, kanala, dile, şehre ve ada göre
süzülür. Teslim edilen satırda "Giden metni gör" o kişiye giden doldurulmuş metni gösterir;
"CSV indir" süzgece uyan bütün alıcıları, yine maskeli adreslerle, dosya olarak verir. Kampanyalar listesi de duruma ve metne göre süzülür. Mock sağlayıcı alıcıların yaklaşık
%2'sini kalıcı olarak reddettiği için her kampanyada birkaç "başarısız" mesaj görülür. Ekranlar adreslenebilir (`#/olustur`, `#/kampanyalar`, `#/kampanya/12`), yani
sayfa yenilense de aynı yerde kalır.

Mock LLM bir dil modeli değil, anahtar kelime eşleştiricidir: şehir, dil, cinsiyet, yaş, sadakat
seviyesi ("altın üyeler"), üyelik tarihi, sepet, satın alma, harcama ve sipariş sayısı
ifadelerini, başka koşul yokken de "herkese" / "tüm kullanıcılara" isteğini tanır; içerik için brief'teki
indirim oranını sabit bir metne yerleştirir (Türkçe ya da İngilizce). Çeviri yapmaz: diğer dil
istendiğinde o dildeki sabit metnini yazar, ana metne elle yapılan değişikliği taşımaz. Çıktısı
gerçek modelinkiyle aynı doğrulamadan geçer. Brief'e `#demo-retry` eklenirse içerik adımında ilk deneme bilerek reddedilir ve yeniden
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
| `POST /api/segments/generate` | `{"brief": "...", "time_zone"?: "Europe/Istanbul"}` → LLM'in önerdiği kural, kuraldan türetilen açıklama, LLM'in yorumları, kanal bazlı kitle sayısı ve ulaşılabilen kitlenin okuduğu diller (`languages`, en kalabalık dil ilk sırada). Format brief'i ifade edemiyorsa `status: "unsupported"`. Brief'te gönderim zamanı varsa `schedule` (öneri) |
| `POST /api/segments/count` | `{"rule": {...}}` → elle düzenlenmiş bir kuralı aynı doğrulayıcıdan geçirir ve yeniden sayar; geçersizse 422 ve `{path, message}` listesi |
| `GET /api/segments/options` | Kural düzenleyicisi için izinli değerler (şehirler, kategoriler, olaylar, metrikler) ve sınırlar; doğrulayıcının kendi tablolarından |
| `POST /api/content/generate` | `{"brief": "...", "language"?: "tr"}` → SMS ve email içeriği, SSE akışı olarak. `language` yazılacak dildir (kitlenin ana dili). Olaylar: `attempt`, `delta` (alan bazlı metin), `rejected` (doğrulama hataları), `result` (doğrulanmış içerik, ölçülen uzunluklar, limitler), `error`. İstek hataları (boş brief vb.) normal JSON hata yanıtıdır |
| `POST /api/content/localize` | `{"brief", "content", "from": "tr", "to": ["en"]}` → `content` içindeki (pazarlamacının son haline getirdiği) metni `to` dillerine uyarlar. Kaç dil istenirse istensin tek model çağrısı. Aynı SSE olayları; `delta` dilini de taşır, `result` dil başına doğrulanmış içerik döner (`variants`) |
| `POST /api/campaigns` | Onay: `{"brief", "rule", "content", "send_at"?}`. `content` tek bir set ya da dil başına bir settir: `{"default": "tr", "variants": {"tr": {...}, "en": {...}}}`; her alıcı kendi dilindeki seti, yoksa `default` dildekini alır. `send_at` (RFC 3339, en fazla 30 gün sonrası) verilirse gönderim o saatte başlar. Kural ve içerik (elle düzenlenmiş olsa da) yeniden doğrulanır; geçerliyse kampanya ve alıcıları kuyruğa girer. `201` ve kanal başına kuyruğa giren mesaj sayısı; geçersizse `422` ve `{path, message}` listesi |
| `GET /api/campaigns` | Son 100 kampanya, en yenisi üstte: brief, başlangıç zamanı, durum (`scheduled`, `sending`, `done`, `cancelled`) ve mesaj sayıları |
| `GET /api/campaigns/{id}` | Gönderim durumu: kanal başına `pending` / `sending` / `sent` / `failed` / `skipped` / `cancelled` sayıları, `send_at`, `cancelled_at`, metin dilleri (`languages`) ve `done` |
| `POST /api/campaigns/{id}/cancel` | Henüz gönderilmemiş mesajları iptal eder; gönderilenler kalır. Tekrarlanabilir |
| `GET /api/campaigns/{id}/recipients` | Kampanyanın alıcıları: ad, şehir, kanal, dil, durum, deneme sayısı, teslim zamanı, varsa son hata. Süzgeçler: `?status=`, `?channel=`, `?language=`, `?city=`, `?q=` (adda arama), `?suppression=` (`unsubscribed` / `invalid_address`); sayfa: `?offset=`, `?limit=` (varsayılan 50, en fazla 100). `by_status` diğer süzgeçlere uyan mesajları duruma göre sayar. Dikkat isteyenler önce gelir; telefon ve email maskelenir. Teslim edilen mesajda `text` (email'de ayrıca `subject`): kişiye giden doldurulmuş metin |
| `GET /api/campaigns/{id}/recipients.csv` | Aynı liste, aynı süzgeçlerle CSV dosyası olarak; sayfa yok, süzgece uyan bütün satırlar. Adresler listede olduğu gibi maskeli; giden konu ve metin dahil (DECISIONS 44) |
| `POST /api/provider/events` | Sağlayıcının webhook'u: `{"type": "unsubscribed", "reference": "campaign-7-user-12-sms"}`. Teslim edilmiş bir mesajın alıcısı abonelikten çıktığında çağrılır; kişi o kanalda bastırılır. `X-Webhook-Secret` başlığı doğrulanır; yinelenen olay zararsızdır |
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

Diğer dilleri yazdırmak için (kaynak metin bir önceki akışın `result` olayındaki `content`):

```bash
curl -sN localhost:8080/api/content/localize -H 'Content-Type: application/json' \
  -d '{"brief":"Sepetine ürün ekleyip almayanlara %30 indirim hatırlatması gönder.",
       "content":{"sms":"Merhaba {{first_name}}, %30 indirim seni bekliyor!","email":{"subject":"%30 indirim","body":"Merhaba {{first_name}}"}},
       "from":"tr","to":["en"]}'
```

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
kabul edip yanıtı "kaybeder" (504) ve her isteği 20–250 ms bekletir. Bunlar geçici hatalardır;
yeniden denemeyle aşılır. Ayrıca alıcıların %2'sini kalıcı olarak reddeder (`422`: numara
kullanımda değil / posta kutusu yok); aynı adres her seferinde reddedilir, worker yeniden denemez
ve mesaj "başarısız" olur (DECISIONS 34). O adres bastırılır: sonraki kampanyalarda kuyruğa
girmez ve kitle ekranında "hariç" olarak sayılır (DECISIONS 35). Ayrıca mesajı teslim ettiği
alıcıların %2'si birkaç saniye sonra abonelikten çıkar; sağlayıcı bunu API'nin webhook'una
bildirir ve o kişi o kanalda bir daha mesaj almaz (DECISIONS 37, `MOCK_UNSUBSCRIBE_RATE`). Oranlar `.env` içindeki `MOCK_*` değişkenleriyle
değişir (`MOCK_UNREACHABLE_RATE=0` kalıcı reddi kapatır).

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
  internal/content içerik şablonları: doğrulama, limit ölçümü, akışlı üretim, diğer dillere uyarlama
  internal/validation ortak doğrulama hatası biçimi ({path, message} listesi)
  internal/metrics sayaçlar ve Prometheus metin çıktısı
  internal/llm     LLM arayüzü; llm/anthropicllm Claude istemcisi, llm/mockllm key yokken kullanılan mock
  internal/httpapi HTTP handler'ları
  internal/testdb  entegrasyon testleri için izole şema
frontend/          Vue 3 + TypeScript + Pinia arayüzü (nginx ile sunulur)
  src/stores       tüm akışın durumu (Pinia)
  src/api          API istemcisi ve tipleri
  src/lib          SSE ayrıştırıcı, şablon ölçümü (canlı sayaç), kural düzenleyicisinin mantığı,
                   adres (hash) okuma / yazma, açılış sahnesinin zaman çizelgesi, tema
  src/components   adım ekranları
.github/workflows/ CI
ai-sessions/       Claude oturum export'ları
```

## Veri modeli

- **users**: ad, soyad, email, telefon, şehir (81 il; DECISIONS 39), dil (`tr` / `en`), cinsiyet, doğum yılı,
  kayıt tarihi, kanal izinleri (`email_opt_in`, `sms_opt_in`), sadakat seviyesi (`loyalty_tier`: bronz / gümüş / altın;
  toplam alışveriş tutarından türetilir, 3000 ve 8000 TL eşikleriyle; DECISIONS 45).
- **events**: `cart_add` ve `purchase` olayları; kategori, tutar, zaman.
- **campaigns**: brief, onaylanan kural ve içerik (JSON; içerik dil başına bir set), kitlenin hesaplandığı an, gönderimin
  başlayacağı an, iptal edildiyse iptal anı.
- **messages**: kampanya × kullanıcı × kanal; durum (`pending`, `sending`, `sent`, `failed`,
  `skipped`, `cancelled`), deneme sayısı, bir sonraki deneme zamanı; teslim edildiyse kişiye giden doldurulmuş metin
  (`sent_subject`, `sent_body`; DECISIONS 44). `UNIQUE (campaign_id, user_id, channel)`.

Seed deterministiktir: aynı `SEED_USERS` / `SEED_RANDOM` her makinede aynı veriyi üretir. Olay
zamanları seed'in yüklendiği ana göredir.

## Testler

| Ne | Nerede |
|---|---|
| Kural doğrulama, SQL derleme, enjeksiyon denemeleri | `backend/internal/segment` |
| Derlenen SQL'in Go'da yazılmış referans hesapla karşılaştırılması; 381 / 222 / 254 / 58 | `backend/internal/audience` |
| İçerik limitleri, şablonlar, akış çözücü, yeniden deneme (bozuk JSON, uzun SMS, bilinmeyen değişken); diğer dillerin tek çağrıda yazılması, dil başına doğrulama | `backend/internal/content` |
| Aynı kişiye iki kez gitmeme (iki worker, hata veren provider), hız limiti, retry, `Retry-After`; zamanı gelmeden gönderilmeme, iptalden sonra hiçbir şeyin gitmemesi; her alıcının kendi dilindeki metni alması | `backend/internal/delivery` |
| Claude istemcisi (yerel bir SSE sunucusuna karşı, key olmadan) | `backend/internal/llm/anthropicllm` |
| HTTP uçları, SSE akışları, onayda yeniden doğrulama | `backend/internal/httpapi` |
| Arayüz: SSE ayrıştırıcı, canlı sayaç ölçümü, kural düzenleyicisinin mantığı, akışın durumu, alıcı listesi, açılış sahnesinin zaman çizelgesi (aynı anda tek başlık, geri sarma) | `frontend/src/**/*.test.ts` |

CI (GitHub Actions) her push'ta üç iş çalıştırır: backend (gofmt, vet, yarış dedektörüyle
testler, Postgres'e karşı), frontend (tip kontrolü, testler, derleme) ve `docker compose up` ile
kurulumun kendisi.

Uç durum brief seti (51 brief: yazım, veride olmayan istekler, çelişkiler, sınır değerleri,
enjeksiyon denemeleri) `python scripts/brief-seti.py` ile çalışan bir yığına karşı koşturulur;
ne denediği ve ilk çalıştırmada ne bulunduğu `docs/brief-seti.md` içinde.

### Bir alıcı listede nasıl görünür

| Durum | Ne oldu | O kampanyada | Sonraki kampanyada |
|---|---|---|---|
| Teslim edildi | Mesaj ulaştı | Teslim sayısında | Olağan alıcı |
| Teslim edildi + "Abonelikten çıktı" | Mesaj ulaştı, kişi sonra o kanaldan çıktı | Teslim sayısında; başarısız değil | Kuyruğa girmez |
| Başarısız | Adres geçersiz (tek deneme) ya da sağlayıcı beş denemede yanıt vermedi | Başarısız sayısında | Adres geçersizse kuyruğa girmez |
| Atlandı | Mesaj kuyruktaydı; gönderilmeden önce kişi çıktı ya da izni kalktı | Atlandı sayısında | — |
| İptal edildi | Kampanya gönderilmeden durduruldu | İptal sayısında | Olağan alıcı |

Kuyruğa girmeyenler sonraki kampanyanın alıcı listesinde yer almaz; kitle ekranında nedenine
göre toplu sayılır ("N mesaj gönderilmeyecek: …"). Hepsi kanal bazındadır (DECISIONS 35, 37).

## Bilinen eksikler

- **Gerçek Claude ile deneme küçük bir örnekle yapıldı (16 Kasım, `claude-opus-5-5`).**
  Geliştirme mock LLM ile yürüdü; teslimden önce gerçek key ile şunlar denendi: beş brief'ten
  kural (örnek brief, iki gruplu "veya", "yalnızca bir kez", kategorili harcama + "cuma sabahı"
  gönderim zamanı, desteklenmeyen bir istek), akışlı Türkçe içerik, İngilizceye yazdırma ve
  431 mesajlık bir kampanyanın gönderimi. Hepsi ilk denemede geçerli çıktı verdi. Bu deneme iki
  hata buldu ve ikisi de düzeltildi (DECISIONS 41): API kural şemasını "gramer çok büyük" diye
  reddediyordu, şema kabul edildikten sonra da model yalnızca "desteklenmiyor" diyebiliyordu.
  Ardından sınırı zorlayan 32 brief daha denendi (iç içe gruplar, XOR, değilleme, çelişki,
  enjeksiyon, İngilizce brief); bulunan iki hata daha düzeltildi: "3'ten fazla" tam 3'ü de
  kapsıyordu, çelişkili brief ve "herkese gönder" ise "yaptı veya yapmadı" koşuluyla herkese
  açılıyordu (DECISIONS 41 eki ve 42). "Herkese gönder" sonradan açık bir `everyone` işaretiyle
  desteklendi ve dokuz brief ile gerçek modelde denendi (DECISIONS 43). Sadakat seviyesi eklenince API
  şemayı yeniden "çok büyük" diye reddetti; kısa kümeli nitelikler modelin şemasında tek seçenekte
  birleştirildi ve 13 brief ile yeniden denendi (DECISIONS 45). Dokuz kuralın sayısı elle yazılmış SQL ile aynı çıktı.
  Denenmeyenler: `docs/brief-seti.md`'deki setin tamamı (o belge yalnızca mock sütununu
  içerir) ve içerik tarafında hata geri besleme turu (kural tarafında bir kez çalıştı). Kitle
  ekranı arayüzden gerçek modelle bir brief için görüldü; mesaj ve gönderim adımları API
  üzerinden denendi (arayüz aynı uçları çağırır).
- Belirsiz cümlelerde gerçek modelin okuması tutarlı değil: "son 30 günde sepete ekleyen veya
  X TL üzeri harcayan" kalıbında dönem bir brief'te yalnızca sepete, bir diğerinde harcamaya da
  uygulandı. Her okuma yorum olarak gösterilir; aynı cümlenin her seferinde aynı kurala
  dönüşeceği garanti değildir.
- Model şeması API'nin gramer sınırına yakın: yeni bir koşul alanı eklenirse gerçek API'de
  yeniden denenmeli. Testler bunu yakalayamaz, yalnızca bilinen iki nedeni (null olabilen alan,
  koşul şemalarının ikinci kopyası) geri gelmesin diye denetler.
- Arayüz masaüstü genişliğinde denendi; dar ekran yalnızca tarayıcı öykünmesiyle, gerçek cihazda
  değil. Varsayılan tema krem; koyu tema üst menüdeki düğmeyle seçilir ve daha az denendi.
- Açılış sayfası case'in istediği bir şey değil, sunum için eklendi; araç ondan bağımsız çalışır
  (DECISIONS 32).
- Alıcı listesi CSV olarak indirilebilir, adresler maskeli (DECISIONS 44); açık adresli dosya ve
  Excel biçimi yok. Giden metin yalnızca 16 Kasım'dan sonra teslim edilen mesajlarda saklıdır.
  Ekrandaki listede gönderim sürerken sayfalar arasında bir satır yer
  değiştirebilir, "Yenile" düzeltir. Ad araması veritabanının harf kurallarına bağlıdır
  (DECISIONS 33).
- Geçersiz çıkan adres bastırılır ve sonraki kampanyalara dahil edilmez (DECISIONS 35), ama
  bastırmayı kaldırmanın arayüzü yok ve tek bir ret yeterli sayılıyor.
- Abonelikten çıkma yalnızca sağlayıcının bildirimiyle işlenir (DECISIONS 37): gelen STOP mesajı
  ayrıştırılmaz, emailde çıkma bağlantısı ve yeniden abone olma yoktur; webhook basit bir
  paylaşılan anahtarla korunur.
- Kampanyada kanal seçilemiyor: her kampanya izni olan herkese hem SMS hem email olarak gider.
  Uç durum brief setinde bulunan diğer açıklar `docs/brief-seti.md` içinde.
- Ekran bileşenleri için otomatik test yok; mantık ve durum birim testleriyle, ekranlar tarayıcıda
  elle denendi.
- Oluşturma adımlarındayken (brief, kitle, içerik) sayfa yenilenirse akış baştan başlar.
  Onaylanan kampanyaların panosu ve liste ise adreslenebilir.
- Kimlik doğrulama yok: arayüze ulaşan herkes her kampanyayı görür ve iptal edebilir.
- Kampanya listesinde sayfalama, arama ve filtre yok (son 100 kampanya).
- Provider sürekli 429 dönerse mesaj süresiz bekler; toplam bekleme için üst sınır yok
  (DECISIONS 21).
- LLM çağrıları için metrik yok, yalnızca log (DECISIONS 23).

## Segment kuralı neleri ifade edebilir

| İstek | Örnek |
|---|---|
| Şehir, dil, cinsiyet (dahil / hariç), yaş aralığı | "Ankara ve İzmir'deki 25-34 yaş kadınlar" |
| Sadakat seviyesi: bronz, gümüş, altın (dahil / hariç; DECISIONS 45) | "Altın üyeler", "gümüş ve üzeri üyeler", "altın üye olmayan kadınlar" |
| Tüm kullanıcılar, yalnızca açıkça istenince (DECISIONS 43) | "Tüm kullanıcılara gönder" |
| Sepete ekleme / satın alma: yaptı ya da yapmadı, son N günde, isteğe bağlı kategori | "Son 7 günde elektronikte sepete ekleyenler", "60 gündür alışveriş yapmayanlar" |
| Olay sayısı: en az, en fazla, tam (DECISIONS 40) | "Son 30 günde en az 3 kez sepete ekleyenler", "son 90 günde yalnızca bir kez alışveriş yapanlar" |
| Üyelik tarihi | "Son 7 günde kayıt olanlar", "1 yıldan eski üyeler" |
| Toplam harcama, sipariş sayısı, ortalama sipariş tutarı (tüm zamanlar ya da son N gün; isteğe bağlı kategori) | "1000 TL üzeri harcayanlar", "en az 5 sipariş verenler", "ortalama sepet tutarı 800 TL üzeri olanlar", "elektronikte 2000 TL üzeri harcayanlar" |
| Koşulların hepsi (VE) ya da herhangi biri (VEYA) | "Kadınlar veya son 7 günde sepete ekleyenler" |
| Tek seviye grup: VE ile VEYA birlikte (DECISIONS 38) | "İstanbul'da yaşayan ve (sepete ekleyen veya 1000 TL üzeri harcayan)" |

Formatın ifade edemediği bir istekte kural üretilmez; arayüz neyin desteklenmediğini gösterir.
Eksik koşullu bir kural daha geniş bir kitle demek olurdu.

## Bonuslar

| Bonus | Durum |
|---|---|
| CI pipeline | Yapıldı (GitHub Actions: backend, frontend, `docker compose up`) |
| Zamanlanmış gönderim | Yapıldı; iptal edilebilir (DECISIONS 27). Brief'teki zaman öneri olarak okunur (DECISIONS 28) |
| Kullanıcının diline göre içerik | Yapıldı (DECISIONS 30): her alıcı kendi dilindeki metni alır. Ana dil için bir model çağrısı, kitledeki diğer dillerin tamamı için bir çağrı |
| A/B varyantları | Yapılmadı (DECISIONS 31). Aşağıda |

### A/B varyantları neden yok

A/B üç parçadır: kitleyi rastgele bölmek, her gruba kendi metnini göndermek ve hangisinin daha
iyi sonuç verdiğini ölçüp kazananı seçmek. İlk ikisi bu sistemde kurulabilir; üçüncüsü
kurulamaz, çünkü ölçecek veri yok: mock sağlayıcı yalnızca "kabul ettim" ya da "hata" der,
açılma ya da tıklama bilgisi üretmez, teslimat oranı da metne bağlı değildir. Kazananı
olmayan bir A/B yarım, rastgele üretilmiş açılma verisiyle seçilen bir kazanan ise yanıltıcı
olurdu. Üstelik özellik içerik şemasına, onaya, worker'a ve panoya, yani çekirdek akışın
tamamına dokunur.

Yapılsaydı: model tek çağrıda iki metin yazar; alıcı onay anında kampanya ve kullanıcı
numarasından türetilen deterministik bir atamayla bir varyanta bağlanır ve atama mesaj satırına
yazılır (yeniden denemede aynı kişi aynı varyantı alır, "iki kez gitmez" kuralı bozulmaz);
worker metni dil ve varyanta göre seçer; sağlayıcının açılma / tıklama geri bildirimleri mesaja
bağlanır ve fark anlamlı olmadan kazanan gösterilmez. Ayrıntısı DECISIONS 31'de.

## Sonraya bırakılanlar

Zaman kalırsa ele alınacaklar ve bilinçli olarak yapılmayanlar.

| Konu | Not |
|---|---|
| İki seviyeden derin VE / VEYA | Tek seviye grup var (DECISIONS 38); "A ve (B veya (C ve D))" gibi daha derin istekler ifade edilemez ve reddedilir |
| Kesin "büyüktür" ("en fazla N kez" ve kategori bazında harcama DECISIONS 40 ile eklendi) | Karşılaştırmalar kapsayıcı; ayrıntılar DECISIONS 25 |
| Ülke, sadakat puanı, cihaz; ürün görüntüleme, uygulama / email açma | Seed verisinde yok |
| Gerçek SMS kodlaması (Türkçe karakterde 70 karakterlik parça) | Limit case'teki gibi düz karakter sayısı (DECISIONS 19) |
| Zamanlanmış kampanyayı düzenleme, alıcının yerel saatinde gönderim | Zamanlanan kampanya yalnızca iptal edilebilir; saat pazarlamacının saat dilimindedir (DECISIONS 27) |
| Türkçe ve İngilizce dışındaki diller | Dil kümesi kapalı ve seed'deki iki dille sınırlı; yeni dil `content.Languages` tablosuna ve `users.language` kısıtına eklenir. Çok dilde tek çağrı yerine dil grupları gerekir (DECISIONS 30) |
| Panoda dil bazında gönderim sayıları | Pano yalnızca kampanyanın hangi dillerde metni olduğunu gösterir |
