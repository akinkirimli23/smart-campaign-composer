"""Checks every kind of audience condition against a running stack.

    python scripts/kural-denemesi.py

For each rule it asks the API for the audience and compares the number with
a count from SQL written here by hand, independently of the rule compiler,
run through `docker compose exec postgres psql`. Then it sends rules that
must be refused and checks where the error points, and finally sends one
grouped campaign and verifies that every queued recipient satisfies the rule.

Needs the compose stack up. It creates one campaign (to the mock provider).
"""
import json
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

sys.stdout.reconfigure(encoding='utf-8')
API = 'http://localhost:8080'


def post(path, body):
    req = urllib.request.Request(API + path, data=json.dumps(body).encode(), headers={'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b'{}')


def get(path):
    with urllib.request.urlopen(API + path, timeout=60) as r:
        return json.loads(r.read())


def sql(query):
    out = subprocess.run(['docker', 'compose', 'exec', '-T', 'postgres', 'psql', '-U', 'scc', '-d', 'scc', '-At', '-c', query],
                         capture_output=True, text=True, encoding='utf-8')
    if out.returncode != 0:
        raise SystemExit('psql: ' + out.stderr)
    return out.stdout.strip()


# --- plain conditions as JSON, and the same thing as hand-written SQL --------------
# T stands for the instant the API evaluated the rule at.
def ev(event, days, exists=True, category=None, min_count=None):
    return {'type': 'event', 'event': event, 'category': category, 'within_days': days, 'exists': exists, 'min_count': min_count}


def ev_sql(event, days, exists=True, category=None, min_count=1):
    cat = " AND e.category = '%s'" % category if category else ''
    n = ("(SELECT count(*) FROM events e WHERE e.user_id = u.id AND e.event_type = '%s'%s"
         " AND e.occurred_at >= T - interval '%d days' AND e.occurred_at <= T)" % (event, cat, days))
    return '%s >= %d' % (n, min_count) if exists else '%s = 0' % n


def metric(name, op, value, days=None):
    return {'type': 'metric', 'metric': name, 'op': op, 'value': value, 'within_days': days}


def metric_sql(name, op, value, days=None):
    agg = 'COALESCE(sum(e.amount), 0)' if name == 'total_spent' else 'count(*)'
    window = " AND e.occurred_at >= T - interval '%d days'" % days if days else ''
    return ("(SELECT %s FROM events e WHERE e.user_id = u.id AND e.event_type = 'purchase' AND e.occurred_at <= T%s) %s %d"
            % (agg, window, '>=' if op == 'gte' else '<=', value))


def attr(field, values, op='in'):
    return {'type': 'attribute', 'field': field, 'op': op, 'values': values}


def attr_sql(field, values, op='in'):
    quoted = ', '.join("'%s'" % v for v in values)
    return 'u.%s %s (%s)' % (field, 'IN' if op == 'in' else 'NOT IN', quoted)


AGE = ({'type': 'attribute', 'field': 'age', 'op': 'between', 'min': 25, 'max': 34},
       '(EXTRACT(year FROM T)::int - u.birth_year) BETWEEN 25 AND 34')
SIGNUP_NEW = ({'type': 'signup', 'op': 'within', 'days': 90}, "u.signup_at >= T - interval '90 days' AND u.signup_at <= T")
SIGNUP_OLD = ({'type': 'signup', 'op': 'older_than', 'days': 365}, "u.signup_at < T - interval '365 days'")
IST = (attr('city', ['İstanbul']), attr_sql('city', ['İstanbul']))
ANK_IZM = (attr('city', ['Ankara', 'İzmir']), attr_sql('city', ['Ankara', 'İzmir']))
NOT_BIG3 = (attr('city', ['İstanbul', 'Ankara', 'İzmir'], 'not_in'), attr_sql('city', ['İstanbul', 'Ankara', 'İzmir'], 'not_in'))
ENGLISH = (attr('language', ['en']), attr_sql('language', ['en']))
FEMALE = (attr('gender', ['female']), attr_sql('gender', ['female']))
CART30 = (ev('cart_add', 30), ev_sql('cart_add', 30))
NO_BUY60 = (ev('purchase', 60, exists=False), ev_sql('purchase', 60, exists=False))
ELEC2 = (ev('purchase', 180, category='electronics', min_count=2), ev_sql('purchase', 180, category='electronics', min_count=2))
SPENT1000 = (metric('total_spent', 'gte', 1000), metric_sql('total_spent', 'gte', 1000))
SPENT_LOW90 = (metric('total_spent', 'lte', 200, 90), metric_sql('total_spent', 'lte', 200, 90))
ORDERS3 = (metric('order_count', 'gte', 3), metric_sql('order_count', 'gte', 3))
NO_ORDERS = (metric('order_count', 'lte', 0), metric_sql('order_count', 'lte', 0))


def at(where, instant):
    """Puts the evaluation instant where the hand-written SQL says T."""
    return re.sub(r'\bT\b', instant, where)


def rule(match, *parts):
    """parts are (json, sql) pairs; returns the rule and its SQL."""
    joiner = ' AND ' if match == 'all' else ' OR '
    return {'match': match, 'conditions': [p[0] for p in parts]}, '(' + joiner.join('(%s)' % p[1] for p in parts) + ')'


def group(match, *parts):
    joiner = ' AND ' if match == 'all' else ' OR '
    return {'type': 'group', 'match': match, 'conditions': [p[0] for p in parts]}, joiner.join('(%s)' % p[1] for p in parts)


CASES = [
    # every plain condition on its own
    ('şehir: içinde', rule('all', IST)),
    ('şehir: birden fazla', rule('all', ANK_IZM)),
    ('şehir: dışında', rule('all', NOT_BIG3)),
    ('dil', rule('all', ENGLISH)),
    ('cinsiyet', rule('all', FEMALE)),
    ('yaş aralığı', rule('all', AGE)),
    ('sepete ekledi (30 gün)', rule('all', CART30)),
    ('satın almadı (60 gün)', rule('all', NO_BUY60)),
    ('kategori + en az 2 kez', rule('all', ELEC2)),
    ('üyelik: son 90 gün', rule('all', SIGNUP_NEW)),
    ('üyelik: 1 yıldan eski', rule('all', SIGNUP_OLD)),
    ('harcama: en az 1000 TL', rule('all', SPENT1000)),
    ('harcama: son 90 günde en fazla 200 TL', rule('all', SPENT_LOW90)),
    ('sipariş: en az 3', rule('all', ORDERS3)),
    ('sipariş: hiç yok', rule('all', NO_ORDERS)),
    # flat combinations
    ('düz VE: şehir + sepet + satın almadı', rule('all', IST, CART30, NO_BUY60)),
    ('düz VEYA: kadın veya yeni üye veya 3 sipariş', rule('any', FEMALE, SIGNUP_NEW, ORDERS3)),
    # one level of grouping, both directions
    ('şehir VE (sepet VEYA harcama)', rule('all', IST, group('any', CART30, SPENT1000))),
    ('(şehir VE kadın) VEYA yeni üye', rule('any', group('all', ANK_IZM, FEMALE), SIGNUP_NEW)),
    ('iki VEYA grubu VE ile', rule('all', group('any', IST, ENGLISH), group('any', NO_ORDERS, NO_BUY60))),
    ('iki VE grubu VEYA ile', rule('any', group('all', FEMALE, ELEC2), group('all', AGE, CART30))),
    ('grup + düz koşullar karışık', rule('all', ENGLISH, group('any', SIGNUP_OLD, ORDERS3), AGE)),
    ('grupta her koşul tipi', rule('all', NOT_BIG3, group('any', ENGLISH, AGE, ELEC2, SIGNUP_NEW, SPENT_LOW90))),
    ('grupta hariç tutma ve "yapmadı"', rule('any', group('all', NOT_BIG3, NO_BUY60), SPENT1000)),
    ('grupta aynı alan, kuralda da', rule('any', IST, group('all', ANK_IZM, FEMALE))),
]

# rules that must be refused, and where the error must point
G = lambda match, *c: {'type': 'group', 'match': match, 'conditions': list(c)}
REFUSED = [
    ('grup içinde grup', {'match': 'all', 'conditions': [IST[0], G('any', CART30[0], G('all', FEMALE[0], AGE[0]))]}, 'conditions[1].conditions[1].type'),
    ('grup kuralla aynı bağlaç', {'match': 'all', 'conditions': [IST[0], G('all', CART30[0], FEMALE[0])]}, 'conditions[1].match'),
    ('tek üyeli grup', {'match': 'all', 'conditions': [IST[0], G('any', CART30[0])]}, 'conditions[1].conditions'),
    ('aynı grup iki kez', {'match': 'all', 'conditions': [G('any', CART30[0], FEMALE[0]), G('any', CART30[0], FEMALE[0])]}, 'conditions[1]'),
    ('grupta veride olmayan şehir', {'match': 'all', 'conditions': [G('any', attr('city', ['Atlantis']), CART30[0])]}, 'conditions[0].conditions[0].values[0]'),
    ('grupta SQL denemesi (alan adı)', {'match': 'all', 'conditions': [G('any', {'type': 'attribute', 'field': "city'; DROP TABLE users;--", 'op': 'in', 'values': ['İstanbul']}, CART30[0])]}, 'conditions[0].conditions[0].field'),
    ('VE grubunda çelişki', {'match': 'any', 'conditions': [G('all', ev('purchase', 30), ev('purchase', 30, exists=False)), IST[0]]}, 'conditions[0].conditions[1]'),
    ('grupta aynı alan iki kez', {'match': 'all', 'conditions': [G('any', IST[0], ANK_IZM[0])]}, 'conditions[0].conditions[1].field'),
    ('grupta bilinmeyen anahtar', {'match': 'all', 'conditions': [{'type': 'group', 'match': 'any', 'conditions': [CART30[0], FEMALE[0]], 'where': '1=1'}]}, 'conditions[0].where'),
    ('10 koşul sınırı (gruplar dahil)', {'match': 'all', 'conditions': [IST[0], FEMALE[0], AGE[0], G('any', *[{'type': 'signup', 'op': 'within', 'days': d} for d in range(10, 90, 10)])]}, 'conditions'),
    ('koşulsuz kural', {'match': 'all', 'conditions': []}, 'conditions'),
]


def main():
    failed = 0

    print('== 1. Kitle sayısı: API ile elle yazılmış SQL ==')
    print('%-46s %8s %8s  %s' % ('kural', 'API', 'SQL', ''))
    for name, (r, where) in CASES:
        status, body = post('/api/segments/count', {'rule': r})
        if status != 200:
            print('%-46s  HATA %d %s' % (name, status, json.dumps(body, ensure_ascii=False)[:120]))
            failed += 1
            continue
        t = "'%s'::timestamptz" % body['as_of']
        want = int(sql('SELECT count(*) FROM users u WHERE ' + at(where, t)))
        got = body['audience']['total']
        ok = got == want and 0 < got < 5000
        failed += not ok
        print('%-46s %8d %8d  %s' % (name, got, want, 'tamam' if ok else 'UYUŞMUYOR' if got != want else 'anlamsız (0 ya da herkes)'))

    print('\n== 2. Reddedilmesi gereken kurallar ==')
    for name, r, path in REFUSED:
        status, body = post('/api/segments/count', {'rule': r})
        details = body.get('error', {}).get('details') or []
        hit = [d for d in details if d['path'] == path]
        ok = status == 422 and bool(hit)
        failed += not ok
        print('%-34s %s  %s' % (name, 'reddedildi' if ok else 'KABUL EDİLDİ / yanlış yer (%d)' % status,
                                (hit[0]['message'] if hit else json.dumps(details, ensure_ascii=False))[:90]))

    print('\n== 3. Gruplu kuralla gerçek gönderim ==')
    r, where = rule('all', ANK_IZM, group('any', CART30, SPENT1000))
    _, seg = post('/api/segments/count', {'rule': r})
    texts = {'default': 'tr', 'variants': {
        'tr': {'sms': 'Merhaba {{first_name}}, deneme.', 'email': {'subject': 'Deneme', 'body': 'Merhaba {{first_name}}'}},
        'en': {'sms': 'Hi {{first_name}}, a test.', 'email': {'subject': 'A test', 'body': 'Hi {{first_name}}'}}}}
    status, created = post('/api/campaigns', {'brief': 'kural denemesi: gruplu kural', 'rule': r, 'content': texts})
    if status != 201:
        print('kampanya oluşturulamadı:', status, created)
        return 1
    cid = created['id']
    while not get('/api/campaigns/%d' % cid)['done']:
        time.sleep(1)
    st = get('/api/campaigns/%d' % cid)
    t = "'%s'::timestamptz" % created['as_of']
    outside = int(sql('SELECT count(*) FROM messages m JOIN users u ON u.id = m.user_id WHERE m.campaign_id = %d AND NOT %s' % (cid, at(where, t))))
    queued = created['queued']
    sent = {ch: c['sent'] + c['failed'] + c['skipped'] for ch, c in st['channels'].items()}
    checks = [
        ('kuyruğa giren = kitle ekranındaki sayı', queued['sms'] == seg['audience']['sms'] and queued['email'] == seg['audience']['email'],
         'kuyruk %s, ekran SMS %d / email %d' % (queued, seg['audience']['sms'], seg['audience']['email'])),
        ('kuralın dışında kalan alıcı yok', outside == 0, '%d alıcı kuralı sağlamıyor' % outside),
        ('her mesaj sonuçlandı', sent['sms'] == queued['sms'] and sent['email'] == queued['email'], str(st['channels'])),
    ]
    for name, ok, detail in checks:
        failed += not ok
        print('%-42s %s  (%s)' % (name, 'tamam' if ok else 'HATA', detail))
    print('kampanya #%d' % cid)

    print('\n%s' % ('HEPSİ GEÇTİ' if not failed else '%d KONTROL BAŞARISIZ' % failed))
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
