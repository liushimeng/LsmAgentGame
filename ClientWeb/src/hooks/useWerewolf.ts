/**
 * 狼人杀 WS 帧订阅与发送 hook
 *
 * 客户端→服务端(§17 同构,使用 `game.werewolf_*` 系列):
 *   - game.join               { room_id, game_kind:"werewolf" }
 *   - game.werewolf_action    { action:"wolf_kill"|"seer_check"|"witch_act"|"guard_protect", target, witch_action, witch_target }
 *   - game.werewolf_vote      { room_id, target }
 *   - game.werewolf_suicide   { room_id }
 *   - game.werewolf_shoot     { room_id, target }
 *   - game.werewolf_sheriff   { room_id, action:"candidate"|"vote"|"elect", target? }
 *   - game.werewolf_finish    { room_id, action:"speak"|"vote"|"start_day"|"idiot_reveal" }
 *   - game.werewolf_sheriff_stream { room_id, slot:1|2, target:-1|0..11 } (2026-07-10 12 人局)
 *   - game.werewolf_idiot_reveal  { room_id, choice:"reveal"|"skip" } (2026-07-10 12 人局)
 *   - game.werewolf_restart_vote { room_id, choice:"yes"|"no"|"abstain" } (2026-07-10)
 *   - game.werewolf_last_words   { room_id, choice:"speak"|"skip", text? } (2026-07-21 §人类玩家)
 *   - game.spectate / unspectate / leave / state
 *
 * 服务端→客户端:
 *   - game.joined / game.peer_joined / game.started
 *   - game.state              (按座位过滤的 ClientGameState)
 *   - game.action_accepted
 *   - game.sheriff_stream_settle (警徽流结算广播,12 人局,§14)
 *   - game.idiot_revealed      (白痴翻牌结果广播,12 人局,§14)
 *   - game.restart_vote_update (增量重开局投票快照;2026-07-10)
 *   - game.over
 *   - game.error
 */

import { useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useWerewolfStore } from '@/store/werewolf.store';
import type { CommentaryLineJSON, PropUseEvent, WerewolfGameState, WerewolfPropKey } from '@/types/werewolf';
import { reportGlobalError } from '@/services/globalError';
import { translate } from '@/i18n';
import { useI18nStore } from '@/store/i18n.store';

type WerewolfFinishAction = 'speak' | 'vote' | 'start_day' | 'idiot_reveal';
type WerewolfSheriffAction = 'candidate' | 'vote' | 'elect';

export function useWerewolf(roomId: string) {
  const {
    setGameState, setMySeat, setGameOver,
    patchSheriffStream, patchIdiotRevealed,
    appendPropEvent, setPreWaitDeadlineAt,
    pushCommentary, setCommentaryFeed,
  } = useWerewolfStore();
  const navigate = useNavigate();
  const roomIdRef = useRef(roomId);
  roomIdRef.current = roomId;
  // 2026-08-11 BUG-ROLE-MISMATCH-P0:同一局内只 toast 一次「自选角色未生效」,
  // 避免 game.state 每 8s 周期刷新 / 每次广播都重复弹全局错误。
  const rolePrefUnmetToastRef = useRef(false);

  useEffect(() => {
    const unsub = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('game.') && env.type !== 'chat.commentary') return;
      const p = env.payload as Record<string, unknown>;
      if (p.room_id && p.room_id !== roomIdRef.current) return;

      switch (env.type) {
        case 'game.joined': {
          const seat = typeof p.my_seat === 'number' ? p.my_seat : -1;
          setMySeat(seat);
          break;
        }
        case 'game.spectated': {
          // 服务端确认观战注册成功。立即请求一次 state,确保不依赖后续的
          // 广播帧(广播可能在 hub 注册完成前发出,或客户端 channel 缓冲
          // 满时被丢弃)。
          setMySeat(-1);
          wsClient.send('game.state', {
            room_id: roomIdRef.current,
            game_kind: 'werewolf',
          });
          break;
        }
        case 'game.state': {
          const gs = p as unknown as WerewolfGameState;
          setGameState(gs);
          // §20260811-09 U1 — 观战者侧解说 feed 一次性灌入(最近 20 条),
          // 后续增量走 chat.commentary + pushCommentary(seq 去重)。
          if (gs.commentary_feed && Array.isArray(gs.commentary_feed)) {
            setCommentaryFeed(gs.commentary_feed);
          }
          if (typeof gs.my_seat === 'number' && gs.my_seat >= 0) {
            setMySeat(gs.my_seat);
          }
          // 2026-08-11 BUG-ROLE-MISMATCH-P0:玩家创建房间选了角色但本局牌组
          // 未抽到(或与他人偏好冲突)时,后端下发 my_role_pref_unmet +
          // my_preferred_role。这里转成全局 toast 让玩家立刻感知「自选未生效」,
          // 不再只能实测发现身份不符(选猎人发猎魔人)却无任何提示。
          // 一局只弹一次(ref 去重),severity=info 不中断操作。
          if (gs.my_role_pref_unmet && !rolePrefUnmetToastRef.current) {
            rolePrefUnmetToastRef.current = true;
            const lang = useI18nStore.getState().lang;
            const roleName = (key?: string) =>
              key ? translate(lang, `werewolf.role.${key}` as any) : '?';
            reportGlobalError({
              message: translate(lang, 'werewolf.rolePick.unmetToast' as any, {
                pref: roleName(gs.my_preferred_role),
                actual: roleName(gs.my_role),
              }),
              severity: 'info',
            });
          }
          // 2026-07-30 BUG-FIX: §130 人类等待窗口 — 收到真正的游戏状态(phase !=
          // filling)时,说明 StartGame 已触发,清零等待 deadline,前端回退到
          // 常规对局 UI(隐藏倒计时等待提示)。
          if (gs.phase && gs.phase !== 'filling') {
            setPreWaitDeadlineAt(null);
          }
          break;
        }
        // 2026-07-30 BUG-FIX: §130 人类等待窗口 — 服务端在 StartGame 之前广播
        // game.pre_wait 帧,告知客户端当前处于 N 秒等待窗口。前端据此把
        // "等待 13 位玩家入座…"改画为"等待人类玩家…(N 秒后自动开始)" + 倒计时,
        // 避免 12AI+1 人类房间永久卡死在"等待入座"。
        case 'game.pre_wait': {
          const deadlineMs = typeof p.deadline_at === 'number' ? p.deadline_at : null;
          setPreWaitDeadlineAt(deadlineMs);
          break;
        }
        case 'game.over': {
          // BUG-R67-P0 (game over UI 10s 同步延迟): setGameOver 仅更新
          // winner,但 WerewolfGamePage 顶部 "对局结束" 横幅依赖
          // gameState.status === 'over' + gameOver,等下一次 game.state 广播
          // (服务端仅在 watchdog tick 触发 gameOverNotified) 才能出现。
          // 修复: 在收到 game.over 帧的同一 tick 把 gameState.status 与
          // phase 同步置为 'over',UI 立即可见。
          const o = p as { winner?: string; status?: string; phase?: string };
          setGameOver({ winner: o.winner ?? '' });
          const cur = useWerewolfStore.getState().gameState;
          if (cur) {
            setGameState({
              ...cur,
              status: o.status ?? 'over',
              phase: (o.phase as WerewolfGameState['phase']) ?? 'over',
            });
          }
          break;
        }
        case 'game.removed': {
          // 房间被管理员强制解散 / 系统清理 → 跳回大厅。
          // 后端 DELETE /api/admin/rooms/:id 会向 hub.rooms ∪ hub.spectators
          // 推 game.removed 帧,所有人都得离开这个房间。
          // BUG-HUNTER-P1-01 (2026-08-07):服务端 SIGTERM 重启路径下 reason 字段
          // 会带"server restart — werewolf rooms do not survive process boundary"
          // 之类说明,但旧实现只 console.warn 后 navigate,玩家在 lobby 看到
          // "Failed to fetch"(其实是后端进程已退出)会一头雾水。这里把 reason
          // 通过全局 toast 报出来,再跳转大厅。
          const removedReason =
            (p as { reason?: string }).reason || '房间已被服务端关闭';
          // eslint-disable-next-line no-console
          console.warn('werewolf: room removed by admin', {
            room_id: roomIdRef.current,
            reason: removedReason,
          });
          reportGlobalError({
            message: `房间已结束:${removedReason}`,
            severity: 'warning',
          });
          navigate('/werewolf');
          break;
        }
        case 'game.error': {
          const e = p as { code: number; message: string };
          // §7.1:服务端错误必须报到全局 toast,不能只 console。
          // 40111 = 死亡玩家尝试使用道具(R173 P2 修复新增 code)。
          reportGlobalError({
            message: e.message || `游戏操作失败(code=${e.code})`,
            severity: 'error',
          });
          break;
        }
        case 'game.restart_vote_update': {
          // 2026-07-10: 增量投票更新。把 yes/no/abstain 明细合并到
          // store.gameState.phase_extra.restart_vote,前端 <WerewolfRestartVotePanel>
          // 据此实时刷新投票人数 + 倒计时。decided=false 时仅合并明细;
          // decided=true 时由随后的 game.state 广播接管(服务端总是先推 update
          // 再推 state 一次)。
          const upd = p as {
            yes?: number[];
            no?: number[];
            abstain?: number[];
            eligible_count?: number;
            yes_quota?: number;
            deadline_at?: string;
            remaining_sec?: number;
            decided?: boolean;
            result?: 'passed' | 'rejected' | 'timeout' | '';
            winner?: string;
          };
          const cur = useWerewolfStore.getState().gameState;
          if (cur) {
            const prevExtra = cur.phase_extra;
            const prevRv = prevExtra?.restart_vote;
            const nextExtra = {
              rounds_total: prevExtra?.rounds_total ?? 0,
              rounds_current: prevExtra?.rounds_current ?? 0,
              speak_count_per_seat: prevExtra?.speak_count_per_seat ?? [],
              grace_remaining_sec: prevExtra?.grace_remaining_sec ?? 0,
              deadline_at: prevExtra?.deadline_at,
              phase_deadline_at: prevExtra?.phase_deadline_at,
              remaining_sec: prevExtra?.remaining_sec,
              death_lyric: prevExtra?.death_lyric,
              dead_list: prevExtra?.dead_list,
              restart_vote: {
                deadline_at: upd.deadline_at ?? prevRv?.deadline_at,
                remaining_sec: upd.remaining_sec ?? prevRv?.remaining_sec ?? 0,
                yes: upd.yes ?? [],
                no: upd.no ?? [],
                abstain: upd.abstain ?? [],
                decided: upd.decided ?? prevRv?.decided ?? false,
                result: (upd.result
                  ? (upd.result as 'passed' | 'rejected' | 'timeout')
                  : prevRv?.result),
                winner: (upd.winner as '' | 'wolf' | 'good' | undefined) ?? prevRv?.winner ?? '',
                eligible_count: upd.eligible_count ?? prevRv?.eligible_count ?? 0,
                yes_quota: upd.yes_quota ?? prevRv?.yes_quota ?? 0,
                my_choice: prevRv?.my_choice,
              },
            };
            useWerewolfStore.getState().setGameState({ ...cur, phase_extra: nextExtra });
          }
          break;
        }
        // 2026-07-10 12 人局 §14: 夜间警长死亡时服务端自动结算警徽流,广播
        // game.sheriff_stream_settle。前端仅镜像状态 + 轻量 console 提示
        // (无全局 toast 服务;全屏横幅由 WerewolfGamePage WerewolfTable 渲染)。
        case 'game.sheriff_stream_settle': {
          const ss = p as {
            dead_seat?: number;
            successor?: number;
            ripped?: boolean;
            reason?: string;
            stream_targets?: number[];
            stream_factions?: number[];
          };
          if (Array.isArray(ss.stream_targets)) {
            patchSheriffStream(ss.stream_targets);
          }
          // eslint-disable-next-line no-console
          console.warn('werewolf: sheriff_stream_settle', {
            room_id: roomIdRef.current,
            dead_seat: ss.dead_seat,
            successor: ss.successor,
            ripped: ss.ripped,
            reason: ss.reason,
          });
          break;
        }
        // 2026-07-10 12 人局 §14: 白痴翻牌结果广播,更新 idiot_revealed_seats +
        // console(IdiotRevealPanel / WerewolfTable 据此渲染动效与徽章)。
        case 'game.idiot_revealed': {
          const ir = p as { seat?: number; revealed?: boolean; choice?: string };
          if (typeof ir.seat === 'number') {
            patchIdiotRevealed(ir.seat, !!ir.revealed);
          }
          // eslint-disable-next-line no-console
          console.warn('werewolf: idiot_revealed', {
            room_id: roomIdRef.current,
            seat: ir.seat,
            revealed: ir.revealed,
            choice: ir.choice,
          });
          break;
        }
        // 2026-07-17 金池结算: 后端结算完成后 per-user 推送的结算明细,
        // 含 result/ante/netGain/finalBalance/winner。写入 store 供
        // WerewolfGamePage 渲染 SettlementModal。
        case 'game.settlement': {
          const st = p as {
            room_id?: string;
            game_kind?: string;
            winner?: string;
            result?: 'win' | 'lose' | 'draw';
            ante?: number;
            netGain?: number;
            finalBalance?: number;
          };
          if (!st || st.room_id !== roomIdRef.current) break;
          useWerewolfStore.getState().setSettlement({
            room_id: st.room_id ?? roomIdRef.current,
            game_kind: st.game_kind ?? 'werewolf',
            winner: st.winner ?? '',
            result: st.result ?? 'draw',
            ante: st.ante ?? 0,
            netGain: st.netGain ?? 0,
            finalBalance: st.finalBalance ?? 0,
          });
          break;
        }
        // 2026-07-21 §13 道具系统 — 道具使用公开广播(独立 WS 帧 game.werewolf_prop_used)。
        // 与 game.state.prop_events[] 二选一(后端至少推一种);appendPropEvent 内部去重。
        case 'game.werewolf_prop_used': {
          const ev = p as Partial<PropUseEvent> & { room_id?: string };
          if (!ev || ev.room_id !== roomIdRef.current) break;
          if (typeof ev.at !== 'number' || typeof ev.from_seat !== 'number' ||
              typeof ev.prop_key !== 'string') break;
          appendPropEvent({
            from_seat: ev.from_seat,
            from_account: ev.from_account,
            prop_key: ev.prop_key as WerewolfPropKey,
            prop_name: ev.prop_name ?? '',
            prop_emoji: ev.prop_emoji ?? '🎴',
            target_seat: typeof ev.target_seat === 'number' ? ev.target_seat : -1,
            price_paid: typeof ev.price_paid === 'number' ? ev.price_paid : 0,
            hit: !!ev.hit,
            effect_text: ev.effect_text,
            phase: ev.phase,
            at: ev.at,
          });
          break;
        }
        // §20260811-09 U1 — spectator-only 解说帧(玩家收不到)。
        case 'chat.commentary': {
          const line = p as unknown as CommentaryLineJSON;
          if (line && typeof line.seq === 'number' && typeof line.text === 'string') {
            pushCommentary(line);
          }
          break;
        }
      }
    });
    return () => unsub();
  }, [
    setGameState, setMySeat, setGameOver,
    patchSheriffStream, patchIdiotRevealed, appendPropEvent,
    setPreWaitDeadlineAt, pushCommentary, setCommentaryFeed, navigate,
  ]);

  const joinGame = useCallback(() => {
    wsClient.send('game.join', { room_id: roomId, game_kind: 'werewolf' });
  }, [roomId]);

  const spectate = useCallback(() => {
    wsClient.send('game.spectate', { room_id: roomId, game_kind: 'werewolf' });
  }, [roomId]);

  const unspectate = useCallback(() => {
    wsClient.send('game.unspectate', { room_id: roomId, game_kind: 'werewolf' });
  }, [roomId]);

  // 2026-07-29 §134 守卫 / 2026-07-30 §198 骑士 / §猎魔人 猎魔人 — 新增 'guard_protect' / 'knight_duel' / 'demon_hunter_hunt' 动作。
  // 字段复用既有 target(对齐 seer_check),不新增 WS 帧。
  //   target = -1(守卫)=空守;target = -1(骑士)=本轮放弃不消耗机会;target = -1(猎魔人)=空过。
  const sendAction = useCallback(
    (
      action:
        | 'wolf_kill'
        | 'guard_protect'
        | 'knight_duel'
        | 'demon_hunter_hunt'
        | 'seer_check'
        | 'witch_act',
      opts: { target?: number; witchAction?: string; witchTarget?: number },
    ) => {
      const payload: Record<string, unknown> = {
        room_id: roomId,
        action,
      };
      if (opts.target !== undefined) payload.target = opts.target;
      if (opts.witchAction !== undefined) payload.witch_action = opts.witchAction;
      if (opts.witchTarget !== undefined) payload.witch_target = opts.witchTarget;
      wsClient.send('game.werewolf_action', payload);
    },
    [roomId],
  );

  const vote = useCallback(
    (target: number) => {
      wsClient.send('game.werewolf_vote', { room_id: roomId, target });
    },
    [roomId],
  );

  const suicide = useCallback(() => {
    wsClient.send('game.werewolf_suicide', { room_id: roomId });
  }, [roomId]);

  // §20260830-02 — 自爆带走:自爆狼(已死)在 suicide_take 阶段选择带走目标;
  // target=-1 = 放弃带走。
  const suicideTake = useCallback(
    (target: number) => {
      wsClient.send('game.werewolf_suicide_take', { room_id: roomId, target });
    },
    [roomId],
  );

  // 2026-07-11: 预言家发起投票
  const proposeVote = useCallback(() => {
    wsClient.send('game.werewolf_propose_vote', { room_id: roomId });
  }, [roomId]);

  const shoot = useCallback(
    (target: number) => {
      wsClient.send('game.werewolf_shoot', { room_id: roomId, target });
    },
    [roomId],
  );

  const sheriff = useCallback(
    (action: WerewolfSheriffAction, target?: number) => {
      const payload: Record<string, unknown> = { room_id: roomId, action };
      if (target !== undefined) payload.target = target;
      wsClient.send('game.werewolf_sheriff', payload);
    },
    [roomId],
  );

  const finish = useCallback(
    (action: WerewolfFinishAction, tiedRound?: number) => {
      const payload: Record<string, unknown> = { room_id: roomId, action };
      if (tiedRound !== undefined) payload.tied_round = tiedRound;
      wsClient.send('game.werewolf_finish', payload);
    },
    [roomId],
  );

  const resign = useCallback(() => {
    // 狼人杀特殊:无"认输"概念;游戏结束只能退出。
    wsClient.send('game.leave', { room_id: roomId });
  }, [roomId]);

  const leaveGame = useCallback(() => {
    wsClient.send('game.leave', { room_id: roomId });
  }, [roomId]);

  // 2026-07-10: 重开局投票。choice ∈ {'yes','no','abstain'};服务端
  // 通过 handleWerewolfRestartVote → Action_RestartVote 自动评估 quorum
  // 并在 passed 时立即开新局。
  const castRestartVote = useCallback(
    (choice: 'yes' | 'no' | 'abstain') => {
      wsClient.send('game.werewolf_restart_vote', { room_id: roomId, choice });
    },
    [roomId],
  );

  // §20260811-01 U2 — 即刻原班重开（快速重开投票）。
  const fastRestart = useCallback(() => {
    wsClient.send('game.werewolf_fast_restart', { room_id: roomId });
  }, [roomId]);

  // 2026-07-10 12 人局 §14: 预言家警长声明 / 撤回警徽流目标。slot=1 第一,
  // slot=2 第二;target=-1 表示撤回该槽位。
  const sheriffStream = useCallback(
    (slot: 1 | 2, target: number) => {
      wsClient.send('game.werewolf_sheriff_stream', { room_id: roomId, slot, target });
    },
    [roomId],
  );

  // 2026-07-10 12 人局 §14: 白痴在 idiot_reveal 阶段选择「翻牌 / 放弃」。
  const idiotReveal = useCallback(
    (choice: 'reveal' | 'skip') => {
      wsClient.send('game.werewolf_idiot_reveal', { room_id: roomId, choice });
    },
    [roomId],
  );

  // 2026-07-21 §人类玩家操作重构: 人类遗言提交 / 放弃。
  // choice="speak" 必带 text;choice="skip" 忽略 text。
  // 服务端 Action_LastWords / Action_SkipLastWords 内部校验 DeathLyricCurrent。
  const lastWords = useCallback(
    (choice: 'speak' | 'skip', text?: string) => {
      const payload: Record<string, unknown> = {
        room_id: roomId,
        choice,
      };
      if (choice === 'speak' && text !== undefined) {
        payload.text = text;
      }
      wsClient.send('game.werewolf_last_words', payload);
    },
    [roomId],
  );

  // 2026-07-21 §13 道具系统 — 玩家发起使用道具。
  // payload 字段对齐后端 handleWerewolfUseProp 严格 JSON 契约(§84b DisallowUnknownFields):
  //   WS 帧 `game.werewolf_use_prop`,payload { room_id, prop_key, target, payload? }。
  //   字段名必须是 `target`(非 target_seat),否则严格解码拒收 → 使用失败。
  //   AOE 道具 target=-1;target 必须为存活玩家座位。
  const useProp = useCallback(
    (propKey: WerewolfPropKey, targetSeat: number, payload?: string) => {
      const body: Record<string, unknown> = {
        room_id: roomId,
        prop_key: propKey,
        target: targetSeat,
      };
      if (payload !== undefined && payload !== '') body.payload = payload;
      wsClient.send('game.werewolf_use_prop', body);
    },
    [roomId],
  );

  const requestState = useCallback(() => {
    wsClient.send('game.state', { room_id: roomId, game_kind: 'werewolf' });
  }, [roomId]);

  return {
    joinGame, spectate, unspectate, sendAction, vote, suicide, suicideTake, shoot, sheriff, finish, resign, leaveGame,
    castRestartVote, fastRestart, sheriffStream, idiotReveal, requestState, proposeVote, lastWords, useProp,
  };
}
