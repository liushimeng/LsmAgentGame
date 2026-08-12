#!/usr/bin/env python3
"""R15 Step 7: Doudizhu play via direct WS + continue game for chinese junqi."""
import json
import ssl
import time
import websocket
import sys
sys.path.insert(0, "/usr/local/LsmWebGame/scripts/r15")

with open("/tmp/r15_accounts.json") as f:
    accounts = json.load(f)

t1 = next(a for a in accounts if a["account"] == "test_01")
t2 = next(a for a in accounts if a["account"] == "test_02")
t3 = next(a for a in accounts if a["account"] == "test_03")

ROOM_ID = "3c4ff96d-6ea6-4612-be15-db59db41150d"

def connect_ws(token):
    ws = websocket.create_connection(
        f"wss://127.0.0.1:39002/ws?token={token}",
        sslopt={"cert_reqs": ssl.CERT_NONE},
        timeout=10,
    )
    return ws

def send_msg(ws, msg_type, payload):
    ws.send(json.dumps({"type": msg_type, "payload": payload}))

def recv_state(ws, timeout=3):
    ws.settimeout(timeout)
    msgs = []
    try:
        while True:
            m = ws.recv()
            d = json.loads(m)
            if d.get("type") == "game.state":
                return d["payload"]
            msgs.append(d)
    except websocket.WebSocketTimeoutException:
        return None
    except Exception as e:
        return None

# Connect all 3
print("=== Connecting WS for all 3 players ===")
ws1 = connect_ws(t1["token"])
ws2 = connect_ws(t2["token"])
ws3 = connect_ws(t3["token"])

# Send game.join
for ws, t in [(ws1, t1), (ws2, t2), (ws3, t3)]:
    send_msg(ws, "game.join", {"room_id": ROOM_ID, "game_kind": "doudizhu"})
    time.sleep(0.5)

# Request state
send_msg(ws1, "game.state", {"room_id": ROOM_ID, "game_kind": "doudizhu"})
state = recv_state(ws1)
print(f"Initial state for test_01: turn={state.get('current_turn') if state else None} my_hand={state.get('my_hand', [])[:5] if state else None}")

# Try sending game.play with one card
if state and state.get("my_hand"):
    first_card = state["my_hand"][0]
    print(f"Playing card: {first_card}")
    send_msg(ws1, "game.play", {"room_id": ROOM_ID, "game_kind": "doudizhu", "cards": [first_card]})
    time.sleep(2)
    state2 = recv_state(ws1)
    print(f"After play: my_hand_count={len(state2.get('my_hand', [])) if state2 else None} last_play={state2.get('last_play') if state2 else None}")

# Close
for ws in [ws1, ws2, ws3]:
    ws.close()
print("Done WS test")