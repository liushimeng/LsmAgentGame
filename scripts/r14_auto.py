#!/usr/bin/env python3
"""Smart doudizhu play - identify hands, plays correctly, finishes game."""
import json
import urllib.request
import time
import base64
import re

DEBUG_BASE = "http://localhost:28999"

def debug_call(action, page_id=None, params=None):
    body = {"action": action}
    if page_id:
        body["page_id"] = page_id
    if params:
        body["params"] = params
    r = urllib.request.Request(f"{DEBUG_BASE}/ControlChromePage", 
                                data=json.dumps(body).encode(),
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        resp_json = json.loads(resp.read())
    if "data" in resp_json and "result" in resp_json["data"]:
        return resp_json["data"]["result"]
    return resp_json

def click_idx(page_id, nth):
    """Click .doudizhu-hand .card-slot at nth index."""
    debug_call("click", page_id, {"selector": f".doudizhu-hand .card-slot:nth-of-type({nth+1})"})

def play_selected(page_id):
    debug_call("click", page_id, {"selector": "button.btn-primary", "nth": 0})

def pass_play(page_id):
    debug_call("click", page_id, {"selector": "button.btn-ghost"})

def get_state(page_id):
    """Returns dict with hand, is_my_turn, buttons."""
    expr = "JSON.stringify({hand:Array.from(document.querySelectorAll('.doudizhu-hand .card-slot')).map(c=>c.textContent.trim()), turn:document.body.innerText.includes('你的回合'), opp:document.body.innerText.includes('对手回合'), btns:Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()), lastPlay:Array.from(document.querySelectorAll('.last-play-cards .doudizhu-card')).map(c=>c.textContent.trim()), gameOver:document.body.innerText.includes('胜利')||document.body.innerText.includes('失败')||document.body.innerText.includes('🏆')})"
    r = debug_call("eval_js", page_id, expr)
    if r and isinstance(r, str):
        return json.loads(r)
    return None

def card_rank(card):
    """Returns rank value (3-15, 16=2, 17=joker small, 18=joker big)."""
    r = card[:-1] if card else ''
    s = card[-1] if card else ''
    ranks = {'3':3,'4':4,'5':5,'6':6,'7':7,'8':8,'9':9,'10':10,'J':11,'Q':12,'K':13,'A':14,'2':15}
    if r in ranks:
        return ranks[r]
    if 'joker_small' in card or '小' in card:
        return 16
    if 'joker_big' in card or '大' in card or '🃏' in card:
        return 17
    return 0

def suit_value(card):
    """Suit value (for tie-breaking): ♠>♥>♣>♦."""
    s = card[-1] if card else ''
    return {'♠':4, '♥':3, '♣':2, '♦':1}.get(s, 0)

def can_beat(card, last_play_card):
    """card > last_play_card?"""
    r1, r2 = card_rank(card), card_rank(last_play_card)
    if r1 > r2: return True
    if r1 < r2: return False
    return suit_value(card) > suit_value(last_play_card)

PAGES = {
    "test_03": "p_d284a3b9",  # landlord
    "test_01": "p_63ad26f4",  # peasant
    "test_02": "p_c1133976",  # peasant
}

# Find which user is landlord
landlord = None
for a, p in PAGES.items():
    s = get_state(p)
    if s:
        hand_str = json.dumps(s.get("hand", []))
        if "👑" in hand_str:
            landlord = a
print(f"Landlord: {landlord}")

def find_lowest_beat(hand, last_play_card):
    """Find lowest card in hand that beats last_play_card. Returns index or None."""
    for i, c in enumerate(hand):
        if c and can_beat(c, last_play_card):
            return i
    return None

def find_lowest(hand):
    """Lowest card index."""
    best_idx = None
    best_rank = 999
    for i, c in enumerate(hand):
        if not c: continue
        r = card_rank(c)
        if r < best_rank:
            best_rank = r
            best_idx = i
        elif r == best_rank and suit_value(c) < suit_value(hand[best_idx] or '3♦'):
            best_idx = i
    return best_idx

# Play loop
print("=== Smart play ===")
moves = 0
while moves < 80:
    # Find who's turn
    active = None
    for a, p in PAGES.items():
        s = get_state(p)
        if s and s.get("turn"):
            active = (a, p, s)
            break
    if not active:
        # Check if game over
        for a, p in PAGES.items():
            s = get_state(p)
            if s and s.get("gameOver"):
                print(f"GAME OVER detected on {a}")
                # Print final text
                full = debug_call("eval_js", p, "document.body.innerText")
                print(full[:1000])
                break
        time.sleep(1)
        continue
    
    a, p, s = active
    hand = s.get("hand", [])
    if len(hand) == 0:
        print(f"  {a}: 0 cards, game should be over")
        time.sleep(2)
        continue
    
    last_play = s.get("lastPlay", [])
    if last_play:
        # Need to beat last_play
        lp_card = last_play[0]  # simple single
        idx = find_lowest_beat(hand, lp_card)
        if idx is None:
            # Can't beat - pass
            pass_play(p)
            print(f"  move {moves+1}: {a} hand={len(hand)} pass (last={lp_card})")
        else:
            click_idx(p, idx)
            time.sleep(0.3)
            play_selected(p)
            print(f"  move {moves+1}: {a} hand={len(hand)} beat {lp_card} with {hand[idx]}")
    else:
        # Free round - play lowest single
        idx = find_lowest(hand)
        if idx is not None:
            click_idx(p, idx)
            time.sleep(0.3)
            play_selected(p)
            print(f"  move {moves+1}: {a} hand={len(hand)} free play {hand[idx]}")
    moves += 1
    time.sleep(1.5)

# Final
print("\n=== Final ===")
for a, p in PAGES.items():
    s = get_state(p)
    print(f"  {a}: hand={len(s.get('hand',[]))}, turn={s.get('turn')}, lastPlay={s.get('lastPlay')}")
    txt = debug_call("eval_js", p, "document.body.innerText")
    if "胜利" in (txt or ""):
        print(f"    WINNER: {a}")
    if "失败" in (txt or ""):
        print(f"    LOSER: {a}")