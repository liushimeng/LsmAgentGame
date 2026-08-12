#!/usr/bin/env python3
"""R15 Step 9: Junqi game via WS - submit layouts and play."""
import json
import ssl
import time
import websocket
import sys

with open("/tmp/r15_accounts.json") as f:
    accounts = json.load(f)

t1 = next(a for a in accounts if a["account"] == "test_01")
t2 = next(a for a in accounts if a["account"] == "test_02")

ROOM_ID = "9bf5b33b-89e3-4c2e-bfa2-714939530345"

def connect_ws(token):
    ws = websocket.create_connection(
        f"wss://127.0.0.1:39002/ws?token={token}",
        sslopt={"cert_reqs": ssl.CERT_NONE},
        timeout=10,
    )
    return ws

def send_msg(ws, msg_type, payload):
    ws.send(json.dumps({"type": msg_type, "payload": payload}))

def recv_all(ws, timeout=3):
    ws.settimeout(timeout)
    msgs = []
    try:
        while True:
            m = ws.recv()
            msgs.append(json.loads(m))
    except Exception:
        pass
    return msgs

def make_layout(color):
    """Build a legal junqi layout.
    Red (y=0..4 is back area? Actually Red: back rows are 0,1; HQ at (0,0) and (4,0))
    Per docs:
    - Red HQ at (0,0) and (4,0); Red back rows y=0,1
    - Black HQ at (0,11) and (4,11); Black back rows y=10,11
    - Bombs not in front row (Red y=5; Black y=6)
    - No piece in camp (行营) - skip those positions
    - Flag must be in HQ

    Camps (行营) on a 5x12 board:
    Red side camps: (1,2), (3,2), (1,4), (3,4)
    Middle camps: (1,5), (3,5), (1,6), (3,6)
    Black side: (1,7), (3,7), (1,9), (3,9)

    Layout: place all 25 pieces in non-camp, non-front-row, valid positions.
    Pieces: 1 Flag, 1 Commander, 1 General, 2 Major, 2 Colonel, 2 Captain, 2 Lieutenant,
            6 Sergeant, 3 Engineer, 2 Bomb, 3 Mine

    Back rows (Red): y=0 (with HQ at x=0,4) and y=1
    Mid rows: y=2,3,4
    """
    placements = []
    # Flag at HQ (0,0)
    placements.append({"type": 1, "at": {"x": 0, "y": 0}})  # Flag in HQ

    # Mines in back 2 rows (y=0,1). HQ cells (0,0)(4,0) are used for HQ pieces, no mine there.
    # Place 3 mines at y=1 (back row): (1,1), (2,1), (3,1)
    for x in [1, 2, 3]:
        placements.append({"type": 11, "at": {"x": x, "y": 1}})  # Mine

    # Second HQ piece (4,0) - put Bomb
    placements.append({"type": 10, "at": {"x": 4, "y": 0}})  # Bomb in HQ

    # Bombs: 2 total. One at (4,0). Need another. Back rows, not in camp.
    # Place bomb at (4,1)
    placements.append({"type": 10, "at": {"x": 4, "y": 1}})

    # Now place high-rank pieces in mid rows (y=2,3,4)
    # Commander at (0,4)
    placements.append({"type": 2, "at": {"x": 0, "y": 4}})  # Commander
    # General at (4,4)
    placements.append({"type": 3, "at": {"x": 4, "y": 4}})  # General
    # 2 Majors at (0,3), (4,3)
    placements.append({"type": 4, "at": {"x": 0, "y": 3}})  # Major
    placements.append({"type": 4, "at": {"x": 4, "y": 3}})  # Major
    # 2 Colonels at (0,2), (4,2)
    placements.append({"type": 5, "at": {"x": 0, "y": 2}})  # Colonel
    placements.append({"type": 5, "at": {"x": 4, "y": 2}})  # Colonel

    # 2 Captains at (1,2), (3,2) - but these are camps! Can't place here.
    # Use mid rows: (2,4) and (3,4)
    placements.append({"type": 6, "at": {"x": 2, "y": 4}})  # Captain
    placements.append({"type": 6, "at": {"x": 3, "y": 4}})  # Captain

    # 2 Lieutenants at (2,3), (3,3)
    placements.append({"type": 7, "at": {"x": 2, "y": 3}})  # Lieutenant
    placements.append({"type": 7, "at": {"x": 3, "y": 3}})  # Lieutenant

    # 6 Sergeants: 6 pieces
    # Row y=2 has camps at (1,2), (3,2). Available: (0,2)? No, used by Colonel.
    # Use y=2 available: (2,2)
    # Use y=4 available: (1,4)? Camp. (3,4)? Camp. (2,4) used.
    # Front row y=5 cannot have bombs but can have other pieces (except no camp restriction either)
    # Place sergeants at y=4: (2,4) used. Try (2,2), (1,3), (3,3)? used
    # Hmm, simpler: just distribute remaining across rows 2-4
    # After pieces placed: y=0: (0,0)Flag, (4,0)Bomb | y=1: 3 mines + (4,1)Bomb
    # y=2: (0,2)Col, (4,2)Col | y=3: (0,3)Maj, (4,3)Maj, (2,3)Lt, (3,3)Lt
    # y=4: (0,4)Cmd, (4,4)Gen, (2,4)Cap, (3,4)Cap
    # Need 6 Sergeants: available non-camp cells:
    # y=2: (1,2)camp, (2,2)free, (3,2)camp → just (2,2)
    # y=3: (1,3)free, (2,3)used, (3,3)used
    # y=4: (1,4)camp, (2,4)used, (3,4)camp
    # So only (2,2) and (1,3) free. Need more. Front row y=5 (no bomb): (0,5)(1,5)camp(2,5)(3,5)camp(4,5)
    # y=5 free: (0,5),(2,5),(4,5). Use these.
    placements.append({"type": 8, "at": {"x": 2, "y": 2}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 1, "y": 3}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 0, "y": 5}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 2, "y": 5}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 4, "y": 5}})  # Sergeant

    # Need 1 more Sergeant (6 total): use y=5 again or front
    placements.append({"type": 8, "at": {"x": 1, "y": 5}})  # Wait (1,5) is camp!

    # Hmm let me redo this more carefully

    return None  # Will redo below


def make_layout_proper(color):
    """Proper legal layout for Red side.
    Camps positions (行营) on 5x12 board:
    Red zone camps: (1,2), (3,2), (1,4), (3,4)
    Middle: (1,5), (3,5), (1,6), (3,6)
    Black zone: (1,7), (3,7), (1,9), (3,9)
    """
    placements = []

    if color == "red":
        # Red HQ at (0,0) and (4,0); back rows y=0,1
        # Flag at HQ
        placements.append({"type": 1, "at": {"x": 1, "y": 0}})  # Flag (0,0) is HQ corner; use (1,0)
        # Actually HQ is exact (0,0) or (4,0). Let me use exact.
        placements.clear()
        placements.append({"type": 1, "at": {"x": 0, "y": 0}})  # Flag at HQ (0,0)

        # 2 Bombs: at (4,0) (other HQ) and at (2,1)
        placements.append({"type": 10, "at": {"x": 4, "y": 0}})  # Bomb at HQ
        placements.append({"type": 10, "at": {"x": 4, "y": 1}})  # Bomb at (4,1)

        # 3 Mines in back rows: (1,1), (2,1), (3,1) (avoiding camps)
        placements.append({"type": 11, "at": {"x": 1, "y": 1}})  # Mine
        placements.append({"type": 11, "at": {"x": 2, "y": 1}})  # Mine
        placements.append({"type": 11, "at": {"x": 3, "y": 1}})  # Mine

        # 3 Engineers (can defuse mines) - in mid rows
        placements.append({"type": 9, "at": {"x": 1, "y": 3}})  # Engineer
        placements.append({"type": 9, "at": {"x": 3, "y": 3}})  # Engineer
        placements.append({"type": 9, "at": {"x": 2, "y": 4}})  # Engineer

        # 1 Commander: (0,4)
        placements.append({"type": 2, "at": {"x": 0, "y": 4}})  # Commander
        # 1 General: (4,4)
        placements.append({"type": 3, "at": {"x": 4, "y": 4}})  # General

        # 2 Major: (0,3), (4,3)
        placements.append({"type": 4, "at": {"x": 0, "y": 3}})  # Major
        placements.append({"type": 4, "at": {"x": 4, "y": 3}})  # Major

        # 2 Colonel: (0,2), (4,2)
        placements.append({"type": 5, "at": {"x": 0, "y": 2}})  # Colonel
        placements.append({"type": 5, "at": {"x": 4, "y": 2}})  # Colonel

        # 2 Captain: (2,2), (2,3)
        placements.append({"type": 6, "at": {"x": 2, "y": 2}})  # Captain
        placements.append({"type": 6, "at": {"x": 2, "y": 3}})  # Captain

        # 2 Lieutenant: (0,5), (4,5)
        placements.append({"type": 7, "at": {"x": 0, "y": 5}})  # Lieutenant
        placements.append({"type": 7, "at": {"x": 4, "y": 5}})  # Lieutenant

        # 6 Sergeants
        placements.append({"type": 8, "at": {"x": 1, "y": 4}})  # WAIT - (1,4) is camp!
        # Recompute. (1,2) camp, (3,2) camp, (1,4) camp, (3,4) camp
        # Available in mid rows (y=2-4):
        # y=2: (0,2)Col used, (2,2)Cap used, (4,2)Col used
        # y=3: (0,3)Maj used, (2,3)Cap used, (4,3)Maj used
        # y=4: (0,4)Cmd used, (2,4)Eng used, (4,4)Gen used
        # So only 1 free mid cell? Hmm need to redistribute

        # Let me restart with a simpler distribution
        pass

    return None


def make_clean_red_layout():
    """Clean Red layout avoiding all camps.
    Camps to avoid: (1,2), (3,2), (1,4), (3,4), (1,5), (3,5), (1,6), (3,6), (1,7), (3,7), (1,9), (3,9)
    Back rows y=0,1 (HQ at x=0,4 for y=0)
    Mid rows y=2,3,4
    Front row y=5 (no bomb allowed, but other pieces ok)
    """
    placements = []

    # y=0: HQ at (0,0) and (4,0)
    placements.append({"type": 1, "at": {"x": 0, "y": 0}})  # Flag at HQ
    placements.append({"type": 10, "at": {"x": 4, "y": 0}})  # Bomb at HQ

    # y=1: free positions (0,1), (1,1), (2,1), (3,1), (4,1)
    placements.append({"type": 11, "at": {"x": 0, "y": 1}})  # Mine
    placements.append({"type": 11, "at": {"x": 1, "y": 1}})  # Mine
    placements.append({"type": 11, "at": {"x": 2, "y": 1}})  # Mine
    placements.append({"type": 10, "at": {"x": 3, "y": 1}})  # Bomb (2nd)
    placements.append({"type": 9, "at": {"x": 4, "y": 1}})  # Engineer

    # y=2: avoid (1,2), (3,2) camps. Available: (0,2), (2,2), (4,2)
    placements.append({"type": 9, "at": {"x": 0, "y": 2}})  # Engineer (2nd)
    placements.append({"type": 9, "at": {"x": 2, "y": 2}})  # Engineer (3rd)
    placements.append({"type": 2, "at": {"x": 4, "y": 2}})  # Commander

    # y=3: all available
    placements.append({"type": 3, "at": {"x": 0, "y": 3}})  # General
    placements.append({"type": 4, "at": {"x": 1, "y": 3}})  # Major
    placements.append({"type": 4, "at": {"x": 2, "y": 3}})  # Major
    placements.append({"type": 5, "at": {"x": 3, "y": 3}})  # Colonel
    placements.append({"type": 5, "at": {"x": 4, "y": 3}})  # Colonel

    # y=4: avoid (1,4), (3,4) camps. Available: (0,4), (2,4), (4,4)
    placements.append({"type": 6, "at": {"x": 0, "y": 4}})  # Captain
    placements.append({"type": 6, "at": {"x": 2, "y": 4}})  # Captain
    placements.append({"type": 7, "at": {"x": 4, "y": 4}})  # Lieutenant

    # y=5: avoid (1,5), (3,5) camps. Available: (0,5), (2,5), (4,5)
    placements.append({"type": 7, "at": {"x": 0, "y": 5}})  # Lieutenant (2nd)
    placements.append({"type": 8, "at": {"x": 2, "y": 5}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 4, "y": 5}})  # Sergeant

    # Need 4 more Sergeants (6 total). Available cells:
    # We've placed 5 in mid rows. Need 4 more sergeants.
    # Available cells not used yet (avoiding camps):
    # Already used: (0,0)(4,0)(0,1)(1,1)(2,1)(3,1)(4,1)(0,2)(2,2)(4,2)(0,3)(1,3)(2,3)(3,3)(4,3)(0,4)(2,4)(4,4)(0,5)(2,5)(4,5)
    # That's 22 cells. Need 25 total - have 23, so 2 more pieces (5 sergeants + need to check count)

    # Wait, let me count placed pieces:
    # Flag(1), Bomb(2), Mine(3), Bomb(4), Engineer(5),
    # Engineer(6), Engineer(7), Commander(8),
    # General(9), Major(10), Major(11), Colonel(12), Colonel(13),
    # Captain(14), Captain(15), Lieutenant(16),
    # Lieutenant(17), Sergeant(18), Sergeant(19)
    # = 19 pieces. Need 6 more.
    # Required: 1+1+1+2+2+2+2+6+3+2+3 = 25. Yes 25 total. So need 6 more Sergeants.

    # Available cells (avoiding camps and already used):
    # y=2 already fully used
    # y=3 fully used
    # y=4 fully used
    # y=5 fully used (3 cells used)
    # y=6: (0,6)(1,6)camp(2,6)(3,6)camp(4,6) - 3 free
    placements.append({"type": 8, "at": {"x": 0, "y": 6}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 2, "y": 6}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 4, "y": 6}})  # Sergeant
    # 3 more needed
    # y=7: (1,7)camp(3,7)camp - (0,7)(2,7)(4,7) free
    placements.append({"type": 8, "at": {"x": 0, "y": 7}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 2, "y": 7}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 4, "y": 7}})  # Sergeant

    # But wait: Bombs cannot be in front row (y=5 for Red).
    # Front row for Red is y=5. Bomb at (3,1) is back row. OK.
    # Also need to verify all positions are not in camps.

    return placements


def make_clean_black_layout():
    """Clean Black layout. Black: HQ at (0,11) and (4,11); back rows y=10,11; front row y=6."""
    placements = []

    # y=11: HQ at (0,11), (4,11)
    placements.append({"type": 1, "at": {"x": 0, "y": 11}})  # Flag at HQ
    placements.append({"type": 10, "at": {"x": 4, "y": 11}})  # Bomb at HQ

    # y=10: free (no camps in y=10)
    placements.append({"type": 11, "at": {"x": 0, "y": 10}})  # Mine
    placements.append({"type": 11, "at": {"x": 1, "y": 10}})  # Mine
    placements.append({"type": 11, "at": {"x": 2, "y": 10}})  # Mine
    placements.append({"type": 10, "at": {"x": 3, "y": 10}})  # Bomb (2nd)
    placements.append({"type": 9, "at": {"x": 4, "y": 10}})  # Engineer

    # y=9: avoid (1,9), (3,9) camps. Available: (0,9), (2,9), (4,9)
    placements.append({"type": 9, "at": {"x": 0, "y": 9}})  # Engineer
    placements.append({"type": 9, "at": {"x": 2, "y": 9}})  # Engineer
    placements.append({"type": 2, "at": {"x": 4, "y": 9}})  # Commander

    # y=8: all available
    placements.append({"type": 3, "at": {"x": 0, "y": 8}})  # General
    placements.append({"type": 4, "at": {"x": 1, "y": 8}})  # Major
    placements.append({"type": 4, "at": {"x": 2, "y": 8}})  # Major
    placements.append({"type": 5, "at": {"x": 3, "y": 8}})  # Colonel
    placements.append({"type": 5, "at": {"x": 4, "y": 8}})  # Colonel

    # y=7: avoid (1,7), (3,7) camps. Available: (0,7), (2,7), (4,7)
    placements.append({"type": 6, "at": {"x": 0, "y": 7}})  # Captain
    placements.append({"type": 6, "at": {"x": 2, "y": 7}})  # Captain
    placements.append({"type": 7, "at": {"x": 4, "y": 7}})  # Lieutenant

    # y=6 (front row for Black): no bombs allowed, but other pieces ok. Avoid (1,6), (3,6) camps
    placements.append({"type": 7, "at": {"x": 0, "y": 6}})  # Lieutenant
    placements.append({"type": 8, "at": {"x": 2, "y": 6}})  # Sergeant
    placements.append({"type": 8, "at": {"x": 4, "y": 6}})  # Sergeant

    # 4 more Sergeants
    placements.append({"type": 8, "at": {"x": 0, "y": 5}})
    placements.append({"type": 8, "at": {"x": 2, "y": 5}})
    placements.append({"type": 8, "at": {"x": 4, "y": 5}})
    placements.append({"type": 8, "at": {"x": 0, "y": 4}})

    # Hmm too many sergeants, missing 1. Let me count
    return None


# Verify layouts
red = make_clean_red_layout()
print(f"Red layout: {len(red)} pieces")
for p in red:
    print(f"  type={p['type']} at=({p['at']['x']},{p['at']['y']})")
# Verify total counts
counts = {}
for p in red:
    counts[p['type']] = counts.get(p['type'], 0) + 1
print(f"Red counts: {counts}")
expected = {1:1, 2:1, 3:1, 4:2, 5:2, 6:2, 7:2, 8:6, 9:3, 10:2, 11:3}
print(f"Expected: {expected}")
print(f"Match: {counts == expected}")