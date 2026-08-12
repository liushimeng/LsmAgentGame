/**
 * Protocol Buffers 消息注册表（前端）
 *
 * 功能：
 *   1. 维护 "消息类型字符串" → "MessageType 构造器" 的映射
 *   2. 提供 proto 帧的编解码工具
 *   3. 与 wsClient 集成，支持 BinaryMessage 收发
 *
 * 迁移策略：
 *   - 初始阶段：JSON 为主，proto 为辅
 *   - 逐步迁移：每个消息类型迁移时在此注册
 *   - 向后兼容：未注册的消息类型继续走 JSON 通道
 */

import { Envelope } from '@/proto/common/envelope';
import { MessageType } from '@protobuf-ts/runtime';

export type ProtoMessageListener<T> = (msg: T, env: Envelope) => void;

interface ProtoRegistration {
  /** 消息类型构造器（用于反序列化） */
  type: MessageType<any>;
  /** 监听器列表 */
  listeners: Set<ProtoMessageListener<any>>;
}

/**
 * Proto 消息注册表
 *
 * 用法示例：
 * ```ts
 * import { protoRegistry } from '@/services/protoRegistry';
 * import { ChatSend } from '@/proto/chat/chat';
 *
 * // 发送
 * protoRegistry.send('chat.send', ChatSend.create({ text: 'hello' }));
 *
 * // 监听
 * protoRegistry.on('chat.message', ChatMessage, (msg, env) => {
 *   console.log(msg.text);
 * });
 * ```
 */
class ProtoRegistry {
  private registrations = new Map<string, ProtoRegistration>();

  /**
   * 注册消息类型（可多次调用添加监听器）
   */
  on<T extends object>(
    msgType: string,
    msgClass: MessageType<T>,
    listener: ProtoMessageListener<T>,
  ): () => void {
    let reg = this.registrations.get(msgType);
    if (!reg) {
      reg = { type: msgClass, listeners: new Set() };
      this.registrations.set(msgType, reg);
    }
    reg.listeners.add(listener);
    return () => reg!.listeners.delete(listener);
  }

  /**
   * 获取指定类型的消息构造器
   */
  getType(msgType: string): MessageType<any> | undefined {
    return this.registrations.get(msgType)?.type;
  }

  /**
   * 分发 proto 消息（由 wsClient 在收到 BinaryMessage 时调用）
   */
  dispatch(env: Envelope): void {
    const reg = this.registrations.get(env.type);
    if (!reg) {
      // 未注册的消息类型 → 静默忽略（由 JSON 通道处理）
      return;
    }
    try {
      const msg = reg.type.fromBinary(env.payload);
      reg.listeners.forEach((fn) => fn(msg, env));
    } catch (e) {
      // eslint-disable-next-line no-console
      console.warn(`[proto] decode error for type ${env.type}`, e);
    }
  }

  /**
   * 检查是否注册了指定类型
   */
  has(msgType: string): boolean {
    return this.registrations.has(msgType);
  }

  /** 列出所有已注册的消息类型 */
  listTypes(): string[] {
    return Array.from(this.registrations.keys());
  }
}

/** 全局单例 */
export const protoRegistry = new ProtoRegistry();

/**
 * 将消息打包为 Envelope 二进制（用于发送）
 */
export function marshalEnvelope(
  type: string,
  msgClass: MessageType<any> | null,
  msg: object | null,
  seq = 0,
): Uint8Array {
  const env: Envelope = { type, seq, payload: new Uint8Array(0) };
  if (msgClass && msg) {
    env.payload = msgClass.toBinary(msg as any);
  }
  return Envelope.toBinary(env);
}

/**
 * 从二进制数据解析 Envelope
 */
export function unmarshalEnvelope(data: ArrayBuffer | Uint8Array): Envelope {
  const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
  return Envelope.fromBinary(bytes);
}
