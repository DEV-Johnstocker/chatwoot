"""Generate synthetic Facebook comments for the fictional Kru Fah Video Lab.

Labels come from construction: each request asks for one bucket of one intent.
Output: one JSON object per line (Case shape for cmd/comment-eval, without draft).
"""
import json, os, sys, time, urllib.request, urllib.error, concurrent.futures as cf

# Usage: OPENROUTER_API_KEY=... python3 synthetic_gen.py out.jsonl
# Every line of the committed synthetic.jsonl was then read and its label
# checked by hand (2026-11-13); regenerating gives a different set.
KEY = os.environ['OPENROUTER_API_KEY']
MODEL = 'google/gemini-3.8-flash'

POSTS = {
    'P1': 'เปิดรับสมัครรุ่นใหม่แล้ว! คอร์สตัดต่อคลิปขายของด้วย CapCut เรียนผ่านมือถือได้ ดูย้อนหลังได้ตลอดชีพ สนใจพิมพ์ F หรือทักแชทรับรายละเอียดเลยค่ะ',
    'P2': '3 ทริคทำซับไตเติ้ลอัตโนมัติใน CapCut ดูจบทำตามได้เลย (คลิปสอนฟรี)',
    'P3': 'ผลงานน้องพลอย ร้านขนมโฮมเมด หลังเรียนกับครูฟ้า 2 สัปดาห์ รีลส์แรกยอดวิวทะลุหมื่น ภูมิใจมากค่ะ',
    'P4': 'คืนนี้ 2 ทุ่ม ไลฟ์สอนตัดคลิปฟรีที่เพจนี้ ใครว่างมาเจอกันนะคะ',
    'P5': 'ตัดต่อเสร็จตีสาม แต่ลืมกดเซฟ 😭 ใครเคยเป็นแบบนี้บ้าง',
    'P6': 'สุขสันต์วันลอยกระทงค่ะ ขอให้ทุกคนขายดี ยอดขายปังๆ 🪷',
}

BUCKETS = {
    'lead': ('lead', 'clear interest in buying or joining the paid course: asks the price, how to sign up or pay, says they want to learn or are interested, asks to be messaged'),
    'lead_terse': ('lead', 'very short buying-interest signals, 1 to 4 words or an emoji that means "me, interested" (for example สนใจ, ib, ทักแชทหน่อย, อยากเรียน, ราคา?, เท่าไหร่คะ, 🙋‍♀️ สนใจ). On the post that says "พิมพ์ F", a bare F or f counts too; never write a bare F for other posts'),
    'question': ('question', 'asks something the business could answer, without saying they want to buy (devices, whether beginners can follow, live replay, which app version, how long a lesson is, a how-to question about the free tip)'),
    'complaint': ('complaint', 'unhappy with the business: messaged and got no reply, live lagged or had no sound, paid but no access link, lesson too fast, bad experience'),
    'praise': ('other', 'praise, thanks, congratulations or greetings with no question and no wish to buy'),
    'tag': ('other', 'tags a friend by name (write the name like "Nok Siriporn" or "@Bee") with at most a few words such as ดูนี่ or 555, without saying they themselves want to buy'),
    'emoji': ('other', 'only emoji, stickers or laughter, for example "😂😂", "[สติกเกอร์]", "👍", "5555555"'),
    'chat': ('other', 'people talking to each other or chit-chat that asks nothing of the business: agreeing, joking, sharing their own story, replying to another commenter'),
    'spam': ('other', 'spam or unrelated selling: like-boosting services, loans, crypto, "click my profile", selling their own unrelated product'),
}

GRID = {
    'P1': dict(lead=10, lead_terse=10, question=8, complaint=3, praise=3, tag=4, emoji=3, chat=3, spam=3),
    'P2': dict(lead=4, lead_terse=3, question=8, complaint=2, praise=6, tag=4, emoji=3, chat=3, spam=2),
    'P3': dict(lead=5, lead_terse=4, question=4, complaint=1, praise=8, tag=3, emoji=3, chat=3, spam=2),
    'P4': dict(lead=3, lead_terse=2, question=6, complaint=4, praise=3, tag=4, emoji=3, chat=4, spam=2),
    'P5': dict(lead=1, question=2, complaint=1, praise=2, tag=4, emoji=5, chat=10, spam=2),
    'P6': dict(lead=1, question=1, complaint=1, praise=4, tag=2, emoji=5, chat=4, spam=3),
}

STYLE = ('Write like real Thai Facebook users, not like an AI: mostly Thai, casual, short; typos and missing tone marks; '
         'wrong ค่ะ/คะ; repeated letters (มากกกก, 5555); no punctuation; some mixed English words; a few in English or Lao. '
         'Vary the length, the gender particles and the wording; never repeat a comment. No names of real people or brands.')


def ask(post_id, bucket, n):
    label, desc = BUCKETS[bucket]
    prompt = (f'A fictional Thai online course "Kru Fah Video Lab" (CapCut video editing for small sellers) posted on its Facebook Page:\n'
              f'"""{POSTS[post_id]}"""\n\nWrite {n} different comments people might leave under this post. Every comment must be of this kind: {desc}.\n'
              f'{STYLE}\nReply with JSON only: {{"comments": ["...", ...]}}')
    body = json.dumps({'model': MODEL, 'temperature': 1.0, 'response_format': {'type': 'json_object'},
                       'messages': [{'role': 'user', 'content': prompt}]}).encode()
    req = urllib.request.Request('https://openrouter.ai/api/v1/chat/completions', data=body,
                                 headers={'Authorization': 'Bearer ' + KEY, 'Content-Type': 'application/json'})
    for attempt in range(6):
        try:
            out = json.load(urllib.request.urlopen(req, timeout=120))
            break
        except urllib.error.HTTPError as e:
            if e.code != 429 or attempt == 5:
                raise
            time.sleep(10 * (attempt + 1))
    text = out['choices'][0]['message']['content']
    text = text[text.index('{'):text.rindex('}') + 1]
    return [(post_id, bucket, label, c.strip()) for c in json.loads(text)['comments'][:n] if c.strip()]


jobs = [(p, b, n) for p, g in GRID.items() for b, n in g.items()]
rows = []
with cf.ThreadPoolExecutor(2) as ex:
    for res in ex.map(lambda j: ask(*j), jobs):
        rows.extend(res)
seen = set()
with open(sys.argv[1], 'w') as f:
    i = 0
    for p, b, label, text in rows:
        if text in seen:
            continue
        seen.add(text)
        i += 1
        f.write(json.dumps({'id': f's{i:03d}', 'name': 'Commenter', 'post': POSTS[p], 'comment': text,
                            'want_intent': label, 'want_pass': False, 'note': f'synthetic {p} {b}'}, ensure_ascii=False) + '\n')
print(len(rows), 'generated,', i, 'unique')
