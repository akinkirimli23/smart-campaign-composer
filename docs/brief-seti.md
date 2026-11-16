# Uç durum brief seti

Sistemi "iyi" brief'lerin dışında denemek için 51 brief. Amaç doğru kitleyi bulduğunu göstermek
değil, **yanlış yaptığı ya da belirsiz kaldığı yerleri görmek**: sessizce daha geniş bir kitle
seçiyor mu, ifade edemediği isteği söylüyor mu, kötü niyetli girdide ne yapıyor.

```bash
docker compose up --build        # yığın ayakta olmalı
python scripts/brief-seti.py     # her brief için sistemin yanıtını yazar
```

Çalıştırıcı yalnızca kitle sorar; kampanya oluşturmaz, mesaj göndermez. Her brief'in yanında
"beklenen" davranış yazar; sonucu bir insan okur. Set, mock LLM ile ve gerçek Claude ile ayrı
ayrı çalıştırılıp iki çıktı karşılaştırılmak üzere tasarlandı.

## Gruplar

| Grup | Ne deniyor | Örnek |
|---|---|---|
| Temel | Desteklenen her koşul tipi | "1000 TL üzeri harcayan ve en az 3 sipariş vermiş…" |
| Yazım | Büyük / küçük harf, Türkçe karaktersiz yazım, bitişik ek, İngilizce brief | "ISTANBUL VE ANKARA KULLANICILARINA…" |
| Veride yok | Şehir, doğum günü, cihaz, sıralama, sadakat seviyesi, açılma / tıklama | "İstanbul ve Van'daki kullanıcılara…" |
| Mantık | VEYA, hariç tutma, çelişki, tek taraflı yaş | "İstanbul dışındaki kullanıcılara…" |
| Herkes | Koşulsuz gönderim isteği | "Tüm kullanıcılara kampanya gönder" |
| Sınır | 0 gün, 5000 gün, 200 yaş, negatif tutar, %150 indirim | "Son 0 günde sepete ekleyenlere…" |
| Zaman | Yarın, dün, 3 ay sonra, "akşam", "Kara Cuma", iki ayrı "3 gün" | "Son 3 günde sepete ekleyen… 3 gün sonra hatırlat" |
| Saldırı | SQL enjeksiyonu, talimat enjeksiyonu, etiket kaçışı, veri sızdırma isteği | "İstanbul'; DROP TABLE users; --…" |
| Gürültü | "merhaba", anlamsız metin, emoji, çok uzun, boş | 2001 karakter |
| Kanal | Yalnızca SMS, WhatsApp | "…yalnızca SMS gönder" |
| Dil | Veride olan ve olmayan dil | "…Almanca konuşan kullanıcılara…" |

Brief'lerin tamamı ve her birinin beklenen davranışı `scripts/brief-seti.py` içindedir.

## İlk çalıştırmada bulunanlar (13 Kasım, mock LLM)

**Beklendiği gibi davrananlar.** Kapalı küme ve sınırlar (veride olmayan şehir reddedildi, kısmi
kural üretilmedi; 0 / 5000 gün ve 999 kez `422`); enjeksiyon denemeleri (SQL metni ve `</brief>`
kaçışı etkisiz; kural yalnızca tanınan şehri içeriyor); "herkese gönder" ve anlamsız girdi
(kural üretilmedi); uzunluk sınırları (2001 karakter ve boş brief `400`); zaman ("dün" ve
"3 ay sonra" için öneri yok, "Kara Cuma" gönderim günü sanılmadı, iki ayrı "3 gün" doğru ayrıldı).

**Hatalı olanlar: mock tanımadığı niteliği sessizce atıyordu.** Dokuz brief istenenden farklı,
çoğu daha geniş bir kitle verdi; en çarpıcısı "İstanbul dışındaki" için İstanbul'u seçmesiydi.
Hepsi düzeltildi: ifade edilebilenler artık ifade ediliyor (şehir hariç tutma, tek taraflı yaş),
edilemeyenler nedeniyle reddediliyor (kanal seçimi, veride olmayan dil, negatif değer, çelişki,
Türkçe olmayan brief). Ayrıntı ve tablo: DECISIONS 36.

**15 Kasım: VEYA.** "Farklı koşullar arası VEYA" brief'i artık reddedilmiyor: kural tek seviye
grup taşıyabiliyor (DECISIONS 38). Sete iki brief eklendi: VE ile VEYA'nın karıştığı ve
gruplamanın yorum olarak gösterilmesi gereken bir brief, ve mock'un hâlâ reddetmesi gereken
üçlü VEYA.

## Açık kalanlar

Bunlar mock'un değil ürünün sınırları; gerçek modelle de geçerli.

- **Kanal seçilemiyor.** Kampanya her zaman izni olan herkese iki kanaldan gider. "Yalnızca SMS"
  isteği şimdilik reddediliyor.
- **Metindeki iddia doğrulanmıyor.** "%150 indirim" kural olarak geçerli; içerik doğrulayıcısı
  uzunluğa ve değişkenlere bakar.
- **Sınır hatasının dili.** "Son 0 günde" için "model geçerli bir kural üretemedi" deniyor;
  pazarlamacıya sınırı söylemek daha yardımcı olurdu.
- **Gerçek Claude ile çalıştırılmadı.** Bu setin asıl değeri orada: modelin "desteklenmiyor"
  deme disiplini, çelişkileri fark etmesi ve talimat enjeksiyonuna direnci yalnızca gerçek
  modelle ölçülebilir. API key gelince ilk yapılacak iş bu.

**15 Kasım: 81 il.** Veri on beş şehirden 81 ile genişledi (DECISIONS 39), bu yüzden "veride
olmayan şehir" brief'leri artık kural üretir. Yerlerine il adlarının sıradan kelimelerle
karışmadığını deneyen brief'ler eklendi: "Muş'taki müşterilere karşı konulmaz…" yalnızca Muş'u
seçer ("müşteri" Muş, "karşı" Kars sayılmaz); "Ordulu", "Urfadakiler", "Antep" gibi ekli ve halk
arasındaki adlar tanınır; "ordusu", "bölüm", "musluk", "ağrısız" şehir sayılmaz.

Bilinen sınır: mock, bir brief'teki şehirlerin ya hepsini dahil eder ya hepsini hariç tutar.
"Ankara dışındaki Urfalılar" gibi ikisini karıştıran bir brief'te iki şehri de hariç tutar;
kitle ekranındaki açıklama bunu gösterir ama doğrusu gerçek modelden beklenir.

**15 Kasım: iki kitleli "veya".** "İstanbul'da … sepete ekleyen veya Ankara'da … 1000 TL üzeri
harcayan" brief'i yanlış okunuyordu (şehirler birleşiyordu). Artık iki yan ayrı kitle sayılır;
ayrıntı DECISIONS 38'in ek notunda.
