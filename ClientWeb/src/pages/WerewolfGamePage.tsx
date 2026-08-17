import { useEffect, useCallback, useState } from 'react';
// 2026-08-06 §房间布局优化 — 折叠状态读取(localStorage 持久化)。
// try/catch 包裹:浏览器隐身模式下 setItem 可能抛 SecurityError,
const readFoldPref = (key: string): boolean => {
  try {
    return typeof window !== 'undefined' && window.localStorage.getItem(key) === '1';
  } catch {
    return false;
  }
};
const writeFoldPref = (key: string, folded: boolean): void => {
  try {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(key, folded ? '1' : '0');
    }
  } catch {
    /* 隐身模式等场景降级 */
  }
};
const LS_FOLD_CHAT = 'werewolf.fold.chat';
const LS_FOLD_INFO = 'werewolf.fold.info';
import { useParams, useNavigate } from 'react-router-dom';
import { useWerewolfStore } from '@/store/werewolf.store';
import { useConnectionStore } from '@/store/connection.store';
import { useWerewolf } from '@/hooks/useWerewolf';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { useAuth } from '@/hooks/useAuth';
import { WerewolfTable } from '@/components/werewolf/WerewolfTable';
import { NightActionPanel } from '@/components/werewolf/NightActionPanel';
import { DayControlPanel } from '@/components/werewolf/DayControlPanel';
import { GameInfoPanel } from '@/components/werewolf/GameInfoPanel';
import { WerewolfGameChatPanel } from '@/components/werewolf/GameChatPanel';
import { ChatQueueModal } from '@/components/werewolf/ChatQueueModal';
import { PhaseClock } from '@/components/werewolf/PhaseClock';
import { WerewolfRestartVotePanel } from '@/components/werewolf/WerewolfRestartVotePanel';
import { SheriffStreamPanel } from '@/components/werewolf/SheriffStreamPanel';
import { IdiotRevealPanel } from '@/components/werewolf/IdiotRevealPanel';
import { GameStatusHeader } from '@/components/werewolf/GameStatusHeader';
import { SettlementModal } from '@/components/ui/SettlementModal';
import { HistoryDrawer } from '@/components/werewolf/HistoryDrawer';
import { SpectatorCompactBar } from '@/components/werewolf/SpectatorCompactBar';
// 2026-08-11 §20260811-05 U2 — 赛后复盘问答面板(终局后向 bot 座位提问)。
import { RecallChatPanel, type BotSeatOption } from '@/components/werewolf/RecallChatPanel';
import { MyTurnIndicator } from '@/components/werewolf/MyTurnIndicator';
import { LastWordsPanel } from '@/components/werewolf/LastWordsPanel';
import { LastWordsStage } from '@/components/werewolf/LastWordsStage';
import PropPanel from '@/components/werewolf/PropPanel';
// 2026-08-10 §20260810-06 — 行为承诺面板 + 按钮。
import { CommitmentPanel } from '@/components/werewolf/CommitmentPanel';
// §20260814-01 U1 — 接线修复:两者的后端路由 / i18n / CSS 早已就绪,
// 唯独没有挂载点,自 §20260812-03 落地起零 import(§126)。
import SecretLetterPanel from '@/components/werewolf/SecretLetterPanel';
import FactionBetPanel from '@/components/werewolf/FactionBetPanel';
import { CommitmentButton } from '@/components/werewolf/CommitmentButton';
// 2026-07-23 §道具特效 — 道具使用视觉特效叠加层(监听 lastPropEvent 触发动画)。
import { PropUseOverlay } from '@/components/werewolf/PropUseOverlay';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { roomService } from '@/services/auth.service';
import { ReadCachedRoomRole } from '@/services/sessionRoomRole';
import { reportGlobalError } from '@/services/globalError';
import { useT } from '@/hooks/useT';
import { phaseLabel } from '@/components/werewolf/phaseLabel';
// 2026-07-22 §任务2 — 身份猜测本地存储 hook(纯前端,不入服务端状态)。
import { useIdentityGuess } from '@/hooks/useIdentityGuess';

/**
 * 狼人杀 7 人标准局 — 游戏对局页
 *
 * §22 教训:进入对局前 reset() 清掉上一个会话残留。
 * §15 教训:AppLayout 唯一持有 WS connection;此页面只 useWerewolf 订阅 game.* 帧。
 * §27 教训:离开/退出房间走 ConfirmModal,不用 window.confirm。
 */
export function WerewolfGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const spectator = useSpectatorMode();
  // 2026-07-09 §13-bugfix — 500K 队列查看面板。点按钮触发,ESC / 点背景关闭。
  const [chatQueueOpen, setChatQueueOpen] = useState(false);
  // 2026-07-18 §UX-运行时 — 对局历史抽屉(顶层 Header 按钮触发)。
  const [historyOpen, setHistoryOpen] = useState(false);
  // 2026-08-06 §房间布局优化 — "房间聊天"右侧折叠 + "房间信息"左侧折叠。
  // 状态从 localStorage 初始化,持久化用户偏好,刷新后保持。
  const [chatFolded, setChatFolded] = useState<boolean>(() => readFoldPref(LS_FOLD_CHAT));
  const [infoFolded, setInfoFolded] = useState<boolean>(() => readFoldPref(LS_FOLD_INFO));
  useEffect(() => { writeFoldPref(LS_FOLD_CHAT, chatFolded); }, [chatFolded]);
  useEffect(() => { writeFoldPref(LS_FOLD_INFO, infoFolded); }, [infoFolded]);
  const toggleChatFold = useCallback(() => setChatFolded((v) => !v), []);
  const toggleInfoFold = useCallback(() => setInfoFolded((v) => !v), []);

  const { gameState, mySeat, gameOver, settlement, reset, busy, setBusy, dismissSettlement, preWaitDeadlineAt } =
    useWerewolfStore();
  // R187-4: 复用 AppLayout 唯一持有的 WS 连接状态(connection.store 由 wsClient.onStatus 驱动),
  // 用于区分「正在连接服务器」与「已连接但游戏状态未到达」。不新建并行 WS。
  const wsStatus = useConnectionStore((s) => s.status);
  const myUserId = useAuth((s) => s.userId);
  // §任务2:身份猜测 hook 的持久化 key —— auth store 未直接暴露 account,
  // 退回到 userId(全局唯一)。如果将来加上 account 字段,这里改用 account 更好。
  // 2026-07-10 §P0-NEW: 500K 队列查看是 admin / super admin 调试功能 — 普通用户
  // 点开后端返 403 "需要管理员权限",体感"按钮无效"。仅 UserTypeAdmin(2)+
  // 才显示按钮,避免误点 + 减少误报。
  const myUserType = useAuth((s) => s.userType);
  const showQueueBtn = (myUserType ?? 1) >= 2;
  const {
    joinGame, spectate, unspectate,
    sendAction, vote, suicide, shoot,
    sheriff, finish,
    leaveGame, requestState,
    castRestartVote, fastRestart, sheriffStream, idiotReveal, proposeVote,
    lastWords, useProp,
  } = useWerewolf(roomId!);

  // 2026-07-22 §任务2 — 玩家身份猜测 hook(纯本地,未登录时降级为 anon)。
  // 观众 (spectator) 也能用(用于复盘),猜测只对自己显示,不影响游戏流程。
  const identityGuess = useIdentityGuess(
    roomId ?? '',
    myUserId ? `uid_${myUserId}` : 'anon_unknown',
  );

  // 2026-07-10 12 人局: 警徽流声明面板显隐 + 白痴翻牌结果已观看标记。
  const [sheriffStreamOpen, setSheriffStreamOpen] = useState(false);
  const [idiotRevealWatched, setIdiotRevealWatched] = useState(false);
  useEffect(() => {
    // 离开 idiot_reveal 阶段时重置观看标记。
    if (gameState?.phase !== 'idiot_reveal') {
      setIdiotRevealWatched(false);
    }
  }, [gameState?.phase]);
  // 监听 IdiotRevealPanel 的 ESC 关闭自定义事件。
  useEffect(() => {
    const onClosed = () => setIdiotRevealWatched(true);
    window.addEventListener('idiot-reveal-closed', onClosed);
    return () => window.removeEventListener('idiot-reveal-closed', onClosed);
  }, []);

  // 入局 / 切视图时 reset
  useEffect(() => {
    if (!roomId) return;
    reset();
    // BUG-R68-P0-1 (2026-07-09 §6.1): WerewolfGamePage previously relied on
    // AppLayout's mount-time wsClient.connect() to have the socket already
    // OPEN by the time this effect fires. That assumption breaks in three
    // scenarios observed in R68:
    //   (a) Auth token was injected directly into localStorage AFTER AppLayout
    //       had already mounted (no token → connect() returned early).
    //   (b) Browser tab was backgrounded long enough for the 5-min reconnect
    //       window to elapse; reconnect loop gave up before the user
    //       navigated here.
    //   (c) Token refresh via authService.refresh() raced the route change
    //       and the new socket wasn't open yet.
    //
    // Mitigation: explicit connect() here (idempotent — ws.ts guards against
    // double-open), plus an onOpen fallback that re-sends the join envelope
    // when the socket transitions to OPEN mid-effect. send() already buffers
    // envelopes when the socket is non-OPEN (R40 P0-2), so the immediate
    // send() below is safe in the OPEN case and harmless in the CONNECTING
    // case (the onOpen listener will re-fire it to cover the
    // token-just-injected scenario).
    wsClient.connect();
    // 2026-07-30 §R210-05: 若 sessionStorage 缓存显示"我已是该房间的 player",
    // 跳过 game.join WS 帧 —— playing 房间的服务端会返 ErrRoomFull (30001),
    // 触发 30012 自我纠正后端会被绕一圈,体感等同"弹窗卡死"。直接走
    // requestState 让 GamePage 拿到现有 in-memory 状态即可。
    const cachedRole = ReadCachedRoomRole(roomId);
    const isLikelyMember = !spectator && (
      cachedRole === 'player' || cachedRole === 'agent'
    );
    const sendJoin = () => {
      if (isLikelyMember) {
        // 已知我是 player,不让服务端再校验;但仍需确保 WS 绑定 — 真实需求
        // 是订阅 game.state 推送,直接 requestState 即可。
        requestState();
        return;
      }
      const frame = spectator ? 'game.spectate' : 'game.join';
      wsClient.send(frame, {
        room_id: roomId,
        game_kind: 'werewolf',
      });
    };
    sendJoin();
    const unsubOpen = wsClient.onOpen(() => {
      // Re-send on every subsequent open too: a reconnect that fires while
      // the user is on this page must re-establish the room binding, not
      // silently leave the player ghost-attached to a stale seat.
      sendJoin();
      // BUG-R133-02 (P2, 2026-07-16): WS reconnect alone doesn't refresh
      // gameState — frames sent during the disconnect window are lost, so
      // the page can stay on "等待游戏开始…" while the server has already
      // moved on. The 8s setInterval below may take up to 8s to fire, so
      // we kick off an immediate `game.state` request right after the
      // socket transitions to OPEN, bounded by the same in-flight guard
      // useWerewolf already imposes (requestState is idempotent and cheap).
      requestState();
    });
    const stateTimer = setInterval(() => requestState(), 8000);
    return () => {
      clearInterval(stateTimer);
      unsubOpen();
      if (spectator) unspectate();
    };
  }, [roomId, spectator, joinGame, spectate, unspectate, requestState, reset]);

  // BUG-R133-01 (P2, 2026-07-16): full-AI 房间创建者(0+13 模式)被服务端降级
  // 为 spectator,但若用户经浏览器直接打开 /werewolf/:roomId(player 路由)而非
  // /werewolf/spectate/:roomId,前端 useSpectatorMode() 返回 false → 发出
  // game.join → 服务端返回 ErrAlreadyInOtherRole(30012)。此时 gameState 永
  // 远为 null,UI 卡在"等待 13 位玩家入座…",体感等同"加入房间弹窗不会消
  // 失"。本 effect 监听 game.error(30012)→ 自动重定向到 spectate 路由并
  // 重连,确保 spectator 列表里有记录的用户能立即看到对局。
  useEffect(() => {
    const unsubErr = wsClient.on((env: WsEnvelope) => {
      if (env.type !== 'game.error') return;
      const p = (env.payload ?? {}) as { code?: number; message?: string };
      if (p.code !== 30012) return;
      // 只在当前是 player 路由时纠正; spectator 路由不会触发 30012。
      if (spectator) return;
      if (!roomId) return;
      // eslint-disable-next-line no-console
      console.warn(
        '[werewolf] game.join → ErrAlreadyInOtherRole; redirecting to spectator route',
        { room_id: roomId, message: p.message },
      );
      nav(`/werewolf/spectate/${roomId}`, { replace: true });
    });
    return () => unsubErr();
  }, [roomId, spectator, nav]);

  // 2026-07-29 §134 守卫 / 2026-07-30 §198 骑士 / §猎魔人 猎魔人 — handleAction 联合类型加
  // 'guard_protect' / 'knight_duel' / 'demon_hunter_hunt'。
  //   guard_protect: target=-1 表空守。
  //   knight_duel: target=-1 表本轮放弃(技能保留);其他值 = 发动决斗。
  //   demon_hunter_hunt: target=-1 表空过;其他值 = 发动狩猎。
  const handleAction = useCallback(
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
      setBusy(true);
      sendAction(action, opts);
      setTimeout(() => setBusy(false), 500);
    },
    [sendAction, setBusy],
  );

  // §198 骑士决斗同日间其他动作一样走 sendAction('knight_duel', { target }),
  // 单独抽出 handler 以保持 DayControlPanel prop 简洁。
  const handleDuel = useCallback(
    (target: number) => {
      handleAction('knight_duel', { target });
    },
    [handleAction],
  );

  const handleVote = useCallback((target: number) => {
    setBusy(true);
    vote(target);
    setTimeout(() => setBusy(false), 500);
  }, [vote, setBusy]);

  // §报告-20260804-03 BUG-02: 此前是本页面**唯一**没有 setBusy 的动作 handler
  // (handleVote / handleFinish / handleAction / handleProposeVote 全都有)。
  // 缺失导致:① 按钮不置灰,玩家点完没有任何「已提交」反馈,与「按钮坏了」
  // 无法区分;② 无双击防抖,连点会重复发帧。
  const handleSheriff = useCallback((action: 'candidate' | 'vote' | 'elect', target?: number) => {
    setBusy(true);
    sheriff(action, target);
    setTimeout(() => setBusy(false), 500);
  }, [sheriff, setBusy]);

  const handleFinish = useCallback(
    (action: 'speak' | 'vote' | 'start_day' | 'idiot_reveal', tiedRound?: number) => {
      setBusy(true);
      finish(action, tiedRound);
      setTimeout(() => setBusy(false), 500);
    },
    [finish, setBusy],
  );

  const handleSheriffStream = useCallback(
    (slot: 1 | 2, target: number) => {
      setBusy(true);
      sheriffStream(slot, target);
      setTimeout(() => setBusy(false), 500);
    },
    [sheriffStream, setBusy],
  );

  const handleIdiotReveal = useCallback(
    (choice: 'reveal' | 'skip') => {
      setBusy(true);
      idiotReveal(choice);
      setTimeout(() => setBusy(false), 500);
    },
    [idiotReveal, setBusy],
  );

  // 2026-07-11: 预言家发起投票
  const handleProposeVote = useCallback(() => {
    setBusy(true);
    proposeVote();
    setTimeout(() => setBusy(false), 500);
  }, [proposeVote, setBusy]);

  // 2026-07-21 §人类玩家操作重构: 人类遗言提交 / 放弃
  const handleLastWordsSpeak = useCallback(
    (text: string) => {
      setBusy(true);
      lastWords('speak', text);
      setTimeout(() => setBusy(false), 500);
    },
    [lastWords, setBusy],
  );

  const handleLastWordsSkip = useCallback(() => {
    setBusy(true);
    lastWords('skip');
    setTimeout(() => setBusy(false), 500);
  }, [lastWords, setBusy]);

  // 2026-07-21 §13 道具系统 — 玩家使用道具(handleUseProp 直接发 WS,后端做硬约束:
  // 阶段/余额/冷却/次数/目标/阵营保护)。失败由 PropPanel 自身的 formError +
  // reportGlobalError 兜底显示,本处仅做 setBusy 防止双击。
  const handleUseProp = useCallback(
    (propKey: string, targetSeat: number, payload?: string) => {
      setBusy(true);
      useProp(propKey as Parameters<typeof useProp>[0], targetSeat, payload);
      setTimeout(() => setBusy(false), 500);
    },
    [useProp, setBusy],
  );

  const handleLeave = useCallback(async () => {
    if (spectator) {
      unspectate();
      try { await roomService.leaveSpectate(roomId!); } catch { /* best-effort */ }
    } else {
      leaveGame();
      try { await roomService.leave(roomId!); } catch { /* best-effort */ }
    }
    reset();
    nav('/werewolf');
  }, [roomId, spectator, leaveGame, unspectate, reset, nav]);

  // 我是狼人 + 当前是发言阶段 → 可自杀
  const mySeatAlive = (() => {
    const ms = gameState?.my_seat ?? -1;
    if (ms < 0 || !gameState) return false;
    return gameState.players[ms]?.alive !== false;
  })();
  // 2026-08-09 §20260808-03 — 死亡守卫统一标志(我作为真人玩家已死亡)。
  // 传给 PropPanel 控制「☠ 已死亡」徽章与按钮 disabled;
  // NightActionPanel/DayControlPanel 内部自检 players[mySeat]?.alive 而不依赖此 prop(更解耦)。
  const iAmDead = !spectator && !!gameState && !mySeatAlive;
  const canSuicide = !spectator && gameState?.phase === 'speak' &&
                     gameState?.my_role === 'werewolf' && mySeatAlive;

  const handleSuicide = useCallback(() => {
    suicide();
  }, [suicide]);

  const handleShoot = useCallback((target: number) => {
    setBusy(true);
    shoot(target);
    setTimeout(() => setBusy(false), 500);
  }, [shoot, setBusy]);

  if (!roomId) {
    return <div className="error">Missing room ID</div>;
  }

  const effectiveSeat = spectator ? -1 : mySeat;
  const maxSeat = gameState?.max_seat ?? 13;
  // 玩家：必须 ready 才能进入对局；观众：服务端一开始就会推 game.state，
  // 但游戏仍处于 filling 阶段 ready=false。让 spectator 直接渲染桌面 +
  // 轻量"等待 7 人入座"覆盖层，而不是黑屏 spinner。
  //
  // BUG-WEREWOLF-SPECTATE-FILLING FIX (Round 24 P0): if status is already
  // 'playing' on the server (e.g. mid-game spectator join), we MUST NOT show
  // the filling overlay even if the engine erroneously reports phase=filling
  // (e.g. post-restart state-recovery regression — see SpectateGame in
  // ServerGo/game/werewolf/room.go). Showing "等待 7 位玩家入座" in that
  // case is a permanent UI deadlock because no more players will ever join.
  const isWaiting = !gameState;
  const t = useT();
  // R187-4: gameState 未到达时细分两种过渡态 —
  //   (a) WS 未 open(connecting/reconnecting/idle/closed) → 「正在连接服务器…」
  //   (b) WS 已 open 但服务端尚未推 game.state → 「正在同步游戏状态…」
  // 二者均不再误导为「等待玩家入座」(真正的 filling 阶段由 showFillingOverlay 负责)。
  // WS 断连的全局 toast 由 AppLayout/ReconnectingOverlay 既有链路负责,此处不重复上报。
  const waitingHint = (() => {
    // Debug-2026-08-12-01 P3-7: 观战者此时并非「观战成功」,而是【尚未收到
    // game.state】的过渡态。旧文案 '👁 观战中…' 既硬编码中文(违反 §12 三语
    // 同步),又把「状态未知」说成「已在观战」,用户无从判断是否卡住。
    // WS 未连通时优先报连接问题,与玩家路径保持一致。
    if (wsStatus !== 'open') return t('werewolf.connecting');
    if (spectator) return t('werewolf.spectatorSyncing');
    return t('werewolf.syncingState');
  })();

  // BUG-R212-P1-03 (2026-07-30): 「⏳ 正在同步游戏状态…」不能是无限 spinner。
  // 后端 §92a 自死锁曾让 game.state 永不下发,此处 8s 轮询照发,但 UI 无任何
  // 升级提示,用户只看到永久转圈、无从判断是网络慢还是服务端挂了(违反 §7.1
  // 「失败必须在当前页最高层级可见」)。超过 STALL_SEC 仍无 gameState 即判定
  // 为「同步异常」,渲染可操作的错误态(重试 / 返回大厅)并上报全局 toast。
  const SYNC_STALL_SEC = 20;
  const [syncStalled, setSyncStalled] = useState(false);
  useEffect(() => {
    if (gameState) {
      // 状态已到达 —— 清除异常标记(覆盖「先卡住后恢复」的情况)。
      setSyncStalled(false);
      return;
    }
    // WS 尚未 open 时不计时:那是连接问题,由 ReconnectingOverlay 负责,
    // 这里只盯「WS 已连上但服务端不给状态」这一种失真。
    if (wsStatus !== 'open') {
      setSyncStalled(false);
      return;
    }
    const timer = setTimeout(() => {
      setSyncStalled(true);
      reportGlobalError({
        message: `房间状态同步超时(${SYNC_STALL_SEC} 秒未收到服务端游戏状态),服务器可能繁忙或异常`,
        severity: 'error',
      });
    }, SYNC_STALL_SEC * 1000);
    return () => clearTimeout(timer);
  }, [gameState, wsStatus]);

  // 手动重试:重新拉一次状态并清除异常态,让计时器重新开始。
  const handleRetrySync = useCallback(() => {
    setSyncStalled(false);
    requestState();
  }, [requestState]);
  const showFillingOverlay =
    !!gameState &&
    gameState.phase === 'filling' &&
    gameState.status !== 'playing';

  // 2026-07-30 BUG-FIX: §130 人类等待窗口 — 倒计时(秒)。preWaitDeadlineAt 非空时
  // 启动 1s interval 倒计时,归零时自动清零让 showFillingOverlay 回退到常规文案。
  // 倒计时文案在 filling 覆盖层里替代"等待 N 位玩家入座…",避免 12AI+1 人类房间
  // 客户端误渲染"等待入座"永久卡死。
  const [preWaitRemainingSec, setPreWaitRemainingSec] = useState<number | null>(null);
  useEffect(() => {
    if (preWaitDeadlineAt == null) {
      setPreWaitRemainingSec(null);
      return;
    }
    const calc = () => Math.max(0, Math.ceil((preWaitDeadlineAt - Date.now()) / 1000));
    setPreWaitRemainingSec(calc());
    const timer = setInterval(() => {
      const sec = calc();
      setPreWaitRemainingSec(sec);
      if (sec <= 0) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  }, [preWaitDeadlineAt]);

  // 2026-07-10: 重开局投票阶段独立分支 — 渲染投票面板 + 不显示其他控制组件。
  const isRestartVote = !!gameState && gameState.phase === 'restart_vote';
  const handleRestartVote = useCallback(
    (choice: 'yes' | 'no' | 'abstain') => castRestartVote(choice),
    [castRestartVote],
  );

  return (
    <div className="werewolf-game">
      {/* §2026-08-07 — 合并主标题 + 法官横幅 + 状态条 为统一可折叠 GameStatusHeader */}
      <div className="werewolf-top-bar">
        {gameState && (
          <GameStatusHeader
            gameState={gameState}
            isHumanInRoom={!spectator}
            spectator={spectator}
            roomId={roomId}
            nowMs={Date.now()}
            onOpenHistory={() => setHistoryOpen(true)}
          />
        )}
      </div>
      <div
        className="game-area"
        data-chat-folded={chatFolded ? '1' : undefined}
        data-info-folded={infoFolded ? '1' : undefined}
      >
        {/* Left column: 「房间信息」 — §2026-08-06 折叠状态由 infoFolded 驱动。
         * 折叠时整个列变成一个 36px 窄条,仅露出左侧◀手柄;展开时还原。 */}
        <aside
          className={`game-sidebar room-info-panel${infoFolded ? ' game-sidebar--folded' : ''}`}
          data-folded={infoFolded ? '1' : undefined}
        >
          {infoFolded ? (
            /* 折叠态:只显示贴左的窄条手柄 */
            <button
              type="button"
              className="room-info-fold-handle"
              onClick={toggleInfoFold}
              data-testid="ww-info-fold-handle"
              aria-label={t('werewolf.fold.infoCollapsed')}
              title={t('werewolf.fold.infoCollapsed')}
            >
              ▶ {spectator ? '👁' : '🏠'}
            </button>
          ) : (
            /* 展开态:显示完整 header + 内容 */
            <>
              <header className="room-info-panel__header">
                <div className="room-info-panel__header-row">
                  <button
                    type="button"
                    className="room-info-panel__fold-btn"
                    onClick={toggleInfoFold}
                    data-testid="ww-info-fold-toggle"
                    aria-label={t('werewolf.fold.infoExpanded')}
                    title={t('werewolf.fold.infoExpanded')}
                  >
                    ◀ {spectator ? '👁 房间信息' : '🏠 房间信息'}
                  </button>
                  <button
                    className="btn btn-secondary room-info-panel__queue-btn"
                    onClick={() => setChatQueueOpen(true)}
                    data-testid="werewolf-chat-queue-button"
                    title="查看房间共享 500K 聊天历史队列 (需要管理员权限)"
                    style={showQueueBtn ? undefined : { display: 'none' }}
                  >
                    📚 500K
                  </button>
                </div>
                <span className="room-info-panel__sub">
                  {phaseLabel(t, gameState?.phase) ?? '—'}
                </span>
              </header>
              <div className="room-info-panel__body">
                <GameInfoPanel
                  gameState={gameState}
                  mySeat={effectiveSeat}
                  spectator={spectator}
                  onLeave={handleLeave}
                />
              </div>
            </>
          )}
        </aside>
        {/* Center column: game board + action panels (10px 紧凑间距由
         * board-container 的 gap 统一控制)。
         * §20260816-02 U6 — cols-N 钩子在 WerewolfTable 内部使用,
         * 本容器直接靠内层 grid 触发紧凑样式级联。*/}
        <div className="board-container">
          {isWaiting ? (
            <div className="waiting-board">
              {syncStalled ? (
                /* BUG-R212-P1-03: 同步超时 → 从「无限 spinner」升级为可操作错误态。 */
                <div className="waiting-board__stalled" data-testid="ww-sync-stalled">
                  <p className="waiting-board__stalled-title">
                    ⚠️ {t('werewolf.syncStalled')}
                  </p>
                  <p className="waiting-board__stalled-hint">
                    {t('werewolf.syncStalledHint')}
                  </p>
                  <div className="waiting-board__stalled-actions">
                    <button
                      type="button"
                      className="btn btn-primary"
                      onClick={handleRetrySync}
                      data-testid="ww-sync-retry"
                    >
                      🔄 {t('werewolf.syncRetry')}
                    </button>
                    <button
                      type="button"
                      className="btn"
                      onClick={() => nav('/werewolf')}
                    >
                      ← {t('werewolf.backToLobby')}
                    </button>
                  </div>
                </div>
              ) : (
                <>
                  <p>{waitingHint}</p>
                  <div className="spinner" />
                </>
              )}
            </div>
          ) : (
            <>
              {/* 2026-07-09 §13 增强 — 阶段时钟(所有阶段都有 phase_extra.phase_deadline_at) */}
              {/* 2026-07-30 方案-20260730-03 Fix-UI2: 终局后 phase_deadline_at 是
                  游戏内快照的过期残留,继续渲染会显示「⌛ 等待阶段推进…」,
                  与 header「对局结束」矛盾 — status==='over' 时隐藏。 */}
              {gameState?.phase_extra && gameState.status !== 'over' && (
                <PhaseClock
                  deadlineAt={gameState.phase_extra.phase_deadline_at}
                  fallbackSec={gameState.phase_extra.remaining_sec}
                  phaseLabel={phaseLabel(t, gameState!.phase) ?? '当前阶段'}
                />
              )}
              {/* 2026-07-21 §人类玩家操作重构 — 「轮到我了」专属指示器。
                  仅当 phase_extra.my_turn_now=true 时渲染。 */}
              <MyTurnIndicator
                myTurnNow={!!gameState?.phase_extra?.my_turn_now}
                myTurnRemainingSec={gameState?.phase_extra?.my_turn_remaining_sec ?? 0}
                mySeat={effectiveSeat}
              />
              <WerewolfTable
                gameState={gameState!}
                mySeat={effectiveSeat}
                spectator={spectator}
                identityGuess={{
                  guesses: identityGuess.guesses,
                  guessableRoles: identityGuess.guessableRoles,
                  onChange: identityGuess.setGuess,
                }}
              />
              {showFillingOverlay && (
                <div className="filling-overlay" data-testid="werewolf-filling-overlay">
                  {/* 2026-07-30 BUG-FIX: §130 人类等待窗口 — 12AI+1 人类房间服务端在
                      StartGame 前进入 N 秒等待窗口,前端若继续渲染"等待 N 位玩家
                      入座…"会永久卡死。preWaitDeadlineAt 非空时改画倒计时等待文案。 */}
                  {preWaitDeadlineAt != null ? (
                    <p>
                      {spectator
                        ? `👁 观战中…`
                        : `🐺 等待人类玩家加入…`}
                      {' '}
                      <span data-testid="ww-human-wait-countdown">
                        ({t('werewolf.humanWaitCountdown', { sec: preWaitRemainingSec ?? '—' })})
                      </span>
                    </p>
                  ) : (
                    <p>{spectator ? `👁 观战中（等待 ${maxSeat} 位玩家入座…）` : `🐺 等待 ${maxSeat} 位玩家入座…`}</p>
                  )}
                  <div className="spinner" />
                </div>
              )}
              {/* 2026-08-05 §02 — 「💬 发言时间线」模块已删除:与右栏「房间聊天」
                  信息完全同源重复(同为公开发言的「谁/何时/说了什么」列表)。
                  发言的非聊天展示面收敛为**座位卡气泡**(空间维度,一眼扫全场),
                  聊天面板保留完整历史流(时间维度,可回溯),两者职责正交。 */}
              {/* 2026-08-08 §20260808-02 — 遗言阶段全员可见进度条(缺陷 A/B)。
                  phase==='death_lyric' 时,存活玩家/死者/观战者在中栏顶部都能看到
                  遗言队列进度 + 当前遗言座位;组件内部以 phase 为唯一渲染条件,
                  观战者无分支差异。LastWordsPanel(本人操作面板)仍在下方 !spectator
                  块内单独渲染。 */}
              <LastWordsStage gameState={gameState!} />
              {!spectator && !showFillingOverlay && (
                <>
                  <NightActionPanel
                    gameState={gameState!}
                    onAction={handleAction}
                    busy={busy}
                  />
                  <DayControlPanel
                    gameState={gameState!}
                    mySeat={effectiveSeat}
                    onVote={handleVote}
                    onSheriff={handleSheriff}
                    onFinish={handleFinish}
                    onOpenSheriffStream={() => setSheriffStreamOpen(true)}
                    onProposeVote={handleProposeVote}
                    onDuel={handleDuel}
                    busy={busy}
                  />
                  {/* 2026-08-10 §20260810-06 — 承诺面板（白天发言阶段展开）。
                      观众可见全部承诺真实状态；玩家仅见自己的承诺+他人 pending。 */}
                  {gameState!.phase === 'speak' && gameState!.commitments && gameState!.commitments.length > 0 && (
                    <CommitmentPanel
                      commitments={gameState!.commitments}
                      mySeat={effectiveSeat}
                      isSpectator={spectator}
                    />
                  )}
                  {/* 2026-08-10 §20260810-06 — 承诺按钮（存活人类玩家白天可用）。 */}
                  {!spectator && !iAmDead && gameState!.phase === 'speak' && (
                    <div style={{ margin: '8px 0' }}>
                      <CommitmentButton
                        roomId={roomId!}
                        mySeat={effectiveSeat}
                        aliveSeats={gameState!.players?.map((p, i) => p.alive ? i : -1).filter(i => i >= 0) ?? []}
                        disabled={busy}
                      />
                    </div>
                  )}
                  {/* 2026-07-21 §13 道具系统 — 仅白天发言阶段对存活人类玩家可用。
                      观众禁道具(用 spectator 短路)。
                      2026-08-09 §20260808-03 — 死后本局仍可见道具面板(显示 ☠ 徽章 +
                      余额/历史 + 全按钮 disabled),便于玩家查看本局道具历史与最终结算。 */}
                  {!spectator && gameState!.phase === 'speak' && (
                    <PropPanel
                      gameState={gameState!}
                      mySeat={effectiveSeat}
                      myRole={gameState!.my_role}
                      myFaction={gameState!.my_faction}
                      onUseProp={handleUseProp}
                      busy={busy}
                      iAmDead={iAmDead}
                    />
                  )}
                  {/* 2026-08-14 §20260814-01 U1 — 接线修复:暗线信件 + 阵营赌注。
                      两个组件自 §20260812-03 U2/U3 落地起**从未被任何文件 import**
                      —— 后端路由(router.go:197-201)、三语 i18n(14 键)、
                      CSS(werewolf-20260812-03.css,34 条规则)全部就绪,
                      只差这个挂载点(§126「组件存在但未被 import 等于不存在」)。
                      窗口与后端校验一致:白天 speak 阶段 + 存活人类玩家。 */}
                  {!spectator && !iAmDead && (
                    <>
                      <SecretLetterPanel
                        roomId={roomId!}
                        mySeat={effectiveSeat}
                        aliveSeats={gameState!.players?.map((p, i) => p.alive ? i : -1).filter(i => i >= 0) ?? []}
                        windowOpen={gameState!.phase === 'speak'}
                        t={t}
                      />
                      <FactionBetPanel
                        roomId={roomId!}
                        mySeat={effectiveSeat}
                        aliveSeats={gameState!.players?.map((p, i) => p.alive ? i : -1).filter(i => i >= 0) ?? []}
                        windowOpen={gameState!.phase === 'speak'}
                        t={t}
                      />
                    </>
                  )}
                  {canSuicide && (
                    <button
                      className="btn btn-danger suicide-btn"
                      onClick={handleSuicide}
                      data-testid="werewolf-suicide-button"
                    >
                      💣 {t('werewolf.action.wolfSuicide')}
                    </button>
                  )}
                  {gameState!.hunter_pending && gameState!.my_role === 'hunter' && (
                    <HunterShootInline gameState={gameState!} onShoot={handleShoot} />
                  )}
                  {/* 2026-07-21 §人类玩家操作重构 — 人类遗言面板。
                      仅 death_lyric + my_seat === DeathLyricCurrent 时渲染。 */}
                  {!spectator && (
                    <LastWordsPanel
                      gameState={gameState!}
                      onSpeak={handleLastWordsSpeak}
                      onSkip={handleLastWordsSkip}
                      busy={busy}
                    />
                  )}
                </>
              )}
              {/* 2026-07-30 方案-20260730-03 Fix-UI3: 双数据源兜底 —
                  game.over 帧(gameOver.winner)或 game.state.winner 任一可用
                  即渲染胜方横幅;此前仅依赖 game.over 帧,而狼人杀终局几乎不
                  广播该帧(引擎 checkWinner + watchdog 推进路径),横幅死代码。 */}
              {gameState!.status === 'over' && !isRestartVote && (gameOver?.winner || gameState!.winner) && (
                <div className="game-over-banner">
                  <p>🏆 胜者: {gameOver?.winner || gameState!.winner}</p>
                </div>
              )}
              {isRestartVote && (
                <WerewolfRestartVotePanel
                  gameState={gameState!}
                  onCast={handleRestartVote}
                />
              )}
              {/* 2026-07-10 12 人局: 白痴翻牌阶段 — 全屏遮罩面板(仅决策时渲染,
                  结果态服务端广播 game.idiot_revealed 后 GamePage 退出该阶段)。 */}
              {gameState!.phase === 'idiot_reveal' && !idiotRevealWatched && (
                <IdiotRevealPanel
                  gameState={gameState!}
                  mySeat={effectiveSeat}
                  onChoose={handleIdiotReveal}
                />
              )}
            </>
          )}
        </div>
        {/* Right column: room chat — §2026-08-07 优化后整列结构:
         *  - chatFolded=true  → 36px 窄条手柄「💬 ◀」(列级折叠,localStorage 持久化)
         *  - chatFolded=false → 内层 GameChatPanel 自带 header(单标题 + 单 ▼/▲ 面板级折叠)
         * 已删除 2026-08-06 的 werewolf-chat-wrap__header 双层标题与外层折叠按钮,
         * 仅保留一处标题与一处折叠按钮(面板级 ▼/▲)。Day N 副信息由 GameChatPanel 内部渲染。 */}
        <div
          className={`game-chat-column${chatFolded ? ' game-chat-column--folded' : ''}`}
          data-folded={chatFolded ? '1' : undefined}
        >
          {chatFolded ? (
            /* 折叠态:只显示贴右的窄条手柄 */
            <button
              type="button"
              className="room-chat-fold-handle"
              onClick={toggleChatFold}
              data-testid="ww-chat-fold-handle"
              aria-label={t('werewolf.fold.chatCollapsed')}
              title={t('werewolf.fold.chatCollapsed')}
            >
              💬 ◀
            </button>
          ) : (
            /* 2026-08-07 §房间聊天优化 — 移除冗余 werewolf-chat-wrap__header
             * (含「房间聊天」标题 + 外层折叠按钮 ▶),改用内层 GameChatPanel
             * 自带 header(单标题 + 单 ▼/▲ 面板级折叠),消除双标题/双折叠按钮。
             * Day N 副信息由 WerewolfGameChatPanel 透传给内层渲染。
             * 列级折叠保留(本按钮由 chatFolded 驱动)。 */
            <WerewolfGameChatPanel
              roomId={roomId}
              gameState={gameState}
              myUserId={myUserId}
              isLocalPlayerDead={!spectator && !!gameState && !mySeatAlive}
              currentDay={gameState?.day ?? null}
            />
          )}
        </div>
        {/* §20260812-02 v2 — 观战者紧凑底栏: 解说席 + 观众押注合并为单一组件,
            共享 header 行,内容区水平并排,空态高度压缩到单行。 */}
        {spectator && (
          <div className="spectator-bottom-row">
            <SpectatorCompactBar
              roomId={roomId ?? ''}
              phase={gameState?.phase ?? ''}
              seatCount={(gameState as any)?.max_seat ?? 13}
              playerNames={Object.fromEntries(
                (gameState?.players ?? []).map((p: any) => [p.seat, p.account ?? `${p.seat + 1}号`])
              )}
              onPlaceBet={async (targetSeat, amount) => {
                const { wsClient } = await import('@/services/ws');
                wsClient.send('game.werewolf_bet', {
                  room_id: roomId,
                  target_seat: targetSeat,
                  amount,
                });
              }}
            />
          </div>
        )}
      </div>

      {/* 2026-07-10 12 人局: 警徽流声明面板(模态,ESC 关闭)。 */}
      {sheriffStreamOpen && gameState && (
        <SheriffStreamPanel
          gameState={gameState}
          mySeat={effectiveSeat}
          onDeclare={handleSheriffStream}
          onClose={() => setSheriffStreamOpen(false)}
        />
      )}

      {/* 2026-07-09 §13-bugfix — 500K 队列查看模态框,
       * 由 "📚 500K 队列" 按钮触发,展示房间共享 500K 队列 + 每 bot read pointer */}
      {chatQueueOpen && (
        <ChatQueueModal
          roomId={roomId}
          onClose={() => setChatQueueOpen(false)}
        />
      )}

      {/* 2026-07-18 §UX-运行时 — 对局历史侧滑抽屉(time轴 + bot独白 + 死亡列表 + 整局总结)。
            Header 上的"📜 历史"按钮触发,ESC / 点背景关闭。 */}
      {historyOpen && gameState && (
        <HistoryDrawer
          open={historyOpen}
          onClose={() => setHistoryOpen(false)}
          gameState={gameState}
          spectator={spectator}
        />
      )}

      {/* panels moved into .game-area grid — see spectator-bottom-row below */}

      {/* 2026-07-17 金池结算弹层 — 后端 game.settlement 帧触发。仅人类玩家收到
       * (per-user SendToUser),观战者/机器人不触发。展示底注/净收益/最终余额。 */}
      {settlement && (
        <SettlementModal
          gameKind="werewolf"
          result={settlement.result}
          ante={settlement.ante}
          netGain={settlement.netGain}
          finalBalance={settlement.finalBalance}
          winner={settlement.winner}
          onClose={dismissSettlement}
          onFastRestart={fastRestart}
          // §20260811-02 U2 — 死者身份终局延时揭晓(后端 §20260810-12 D2 已下发,
          // 前端首次消费)。0/undefined 时倒计时整段不渲染。
          deathRevealDelayMin={gameState?.death_reveal_delay_min}
          spectator={spectator}
        />
      )}

      {/* 2026-08-11 §20260811-05 U2 — 赛后复盘问答。终局(over)后对玩家与
       * 观战者开放:向本局任意 bot 座位提问,bot 用冻结 Memory 快照单轮回答。
       * bot 座位列表从 bot_contexts(权威 bot 清单)+ players(role,终局已公开)
       * 合并构造;全 AI 房间观战者同样可用。 */}
      {gameState?.phase === 'over' && (gameState.bot_contexts?.length ?? 0) > 0 && (
        <div className="werewolf-recall-dock">
          <RecallChatPanel
            roomId={roomId ?? ''}
            botSeats={(gameState.bot_contexts ?? []).map((bc): BotSeatOption => {
              const p = gameState.players?.[bc.seat];
              // 终局后存活座位 players[].role 经 RolePubliclyRevealed 全场公开;
              // 死亡座位从 all_dead_list_verbose 兜底(publicRoleName 同源)。
              const dead = gameState.all_dead_list_verbose?.find((d) => d.seat === bc.seat);
              const roleStr = p?.role ?? dead?.role ?? '';
              const roleLabel = roleStr ? t(`werewolf.role.${roleStr}` as any) : '';
              return {
                seat: bc.seat,
                label:
                  `${bc.seat + 1} 号 · ${bc.model || 'AI'}` +
                  (roleLabel ? ` · ${roleLabel}` : ''),
              };
            })}
          />
        </div>
      )}

      {/* 2026-07-23 §道具特效 — 道具使用视觉特效叠加层(监听 lastPropEvent/lastPropSeq
          触发,最少展示 12 秒,pointer-events:none 不阻塞交互)。覆盖整个 werewolf-game 区域。 */}
      {gameState && (
        <PropUseOverlay
          players={gameState.players.map((p, i) => (p ? { seat: i, name: p.agent_name } : null))}
        />
      )}
    </div>
  );
}

/** 猎人开枪的子组件(否则在 Death 时被列出)。 */
function HunterShootInline({
  gameState,
  onShoot,
}: {
  gameState: import('@/types/werewolf').WerewolfGameState;
  onShoot: (target: number) => void;
}) {
  const t = useT();
  return (
    <div className="werewolf-action-panel">
      <h4>🎯 {t('werewolf.action.hunterShootPrompt')}</h4>
      <div className="seat-grid">
        {Array.from({ length: gameState.max_seat }).map((_, seat) => {
          const p = gameState.players[seat];
          if (!p || !p.alive) return null;
          return (
            <button
              key={seat}
              type="button"
              className="seat-chip"
              onClick={() => onShoot(seat)}
              data-testid={`werewolf-hunter-target-${seat}`}
            >
              #{seat + 1}
            </button>
          );
        })}
      </div>
    </div>
  );
}
