import type { MessageEvent } from './types';
import { AgentRing } from './agents';

const PULSE_DURATION = 600; // ms for pulse travel
const FADE_DURATION = 500;  // ms for line fade after pulse

interface ActiveMessage {
  sender: { x: number; y: number };
  recipient: { x: number; y: number };
  color: string;
  msgType: string;
  startTime: number;
}

export class MessageRenderer {
  private activeMessages: ActiveMessage[] = [];

  addMessage(event: MessageEvent, agentRing: AgentRing): void {
    const senderPos = agentRing.getAgentPosition(event.sender);
    const recipientPos = agentRing.getAgentPosition(event.recipient);
    if (!senderPos || !recipientPos) return;

    const color = agentRing.getAgentColor(event.sender);

    this.activeMessages.push({
      sender: senderPos,
      recipient: recipientPos,
      color,
      msgType: event.msgType,
      startTime: Date.now(),
    });
  }

  reset(): void {
    this.activeMessages = [];
  }

  draw(ctx: CanvasRenderingContext2D): void {
    const now = Date.now();
    const totalDuration = PULSE_DURATION + FADE_DURATION;

    // Remove expired messages
    this.activeMessages = this.activeMessages.filter(
      (m) => now - m.startTime < totalDuration
    );

    for (const msg of this.activeMessages) {
      const elapsed = now - msg.startTime;
      const { sender: s, recipient: r } = msg;

      if (elapsed < PULSE_DURATION) {
        // Pulse traveling phase
        const t = elapsed / PULSE_DURATION;

        // Draw line (growing)
        ctx.beginPath();
        ctx.moveTo(s.x, s.y);
        const currentX = s.x + (r.x - s.x) * t;
        const currentY = s.y + (r.y - s.y) * t;
        ctx.lineTo(currentX, currentY);
        ctx.strokeStyle = this.getLineColor(msg, 0.6);
        ctx.lineWidth = this.getLineWidth(msg);
        ctx.stroke();

        // Pulse dot
        ctx.beginPath();
        ctx.arc(currentX, currentY, 4, 0, Math.PI * 2);
        ctx.fillStyle = this.getLineColor(msg, 1);
        ctx.shadowBlur = 10;
        ctx.shadowColor = msg.color;
        ctx.fill();
        ctx.shadowBlur = 0;
      } else {
        // Fading phase
        const fadeT = (elapsed - PULSE_DURATION) / FADE_DURATION;
        const alpha = 1 - fadeT;

        ctx.beginPath();
        ctx.moveTo(s.x, s.y);
        ctx.lineTo(r.x, r.y);
        ctx.strokeStyle = this.getLineColor(msg, alpha * 0.6);
        ctx.lineWidth = this.getLineWidth(msg);
        ctx.stroke();
      }
    }
  }

  private getLineColor(msg: ActiveMessage, alpha: number): string {
    if (msg.msgType === 'input-needed') {
      return `rgba(255, 193, 7, ${alpha})`;
    }
    // Parse hex color to rgba
    const hex = msg.color;
    const rr = parseInt(hex.slice(1, 3), 16);
    const g = parseInt(hex.slice(3, 5), 16);
    const b = parseInt(hex.slice(5, 7), 16);
    return `rgba(${rr}, ${g}, ${b}, ${alpha})`;
  }

  private getLineWidth(msg: ActiveMessage): number {
    switch (msg.msgType) {
      case 'instruction':
        return 2.5;
      case 'state-change':
        return 1.5;
      case 'input-needed':
        return 3;
      default:
        return 2;
    }
  }
}
