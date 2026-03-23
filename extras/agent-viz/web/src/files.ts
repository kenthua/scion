import type { FileEditEvent } from './types';
import type { AgentRing } from './agents';
import type { FileGraph } from './graph';

const PARTICLE_DURATION = 800; // ms for particle to travel
const MATERIALIZE_DURATION = 400; // ms for new file to appear

interface ActiveParticle {
  from: { x: number; y: number };
  to: { x: number; y: number };
  color: string;
  action: 'create' | 'edit' | 'read';
  startTime: number;
  filePath: string;
  revealed: boolean; // whether we've triggered file reveal
}

export class FileEditRenderer {
  private particles: ActiveParticle[] = [];
  private fileGraph: FileGraph | null = null;

  setFileGraph(fg: FileGraph): void {
    this.fileGraph = fg;
  }

  addFileEdit(
    event: FileEditEvent,
    agentRing: AgentRing,
    fileGraph: FileGraph
  ): void {
    const agentPos = agentRing.getAgentPosition(event.agentId);
    if (!agentPos) return;

    const filePos = fileGraph.getNodePosition(event.filePath);
    // If file node doesn't exist in graph, find nearest parent
    let targetPos = filePos;
    if (!targetPos) {
      let parent = event.filePath;
      while (parent.includes('/')) {
        parent = parent.substring(0, parent.lastIndexOf('/'));
        targetPos = fileGraph.getNodePosition(parent);
        if (targetPos) break;
      }
    }
    if (!targetPos) return;

    // Need to convert file graph coordinates to screen coordinates
    const graph = fileGraph.getGraph();
    const screenFrom = agentPos;
    const screenTo = graph.graph2ScreenCoords(targetPos.x, targetPos.y);

    const color = agentRing.getAgentColor(event.agentId);

    // Reads travel from file to agent; edits from agent to file
    const isRead = event.action === 'read';
    this.particles.push({
      from: isRead ? { x: screenTo.x, y: screenTo.y } : screenFrom,
      to: isRead ? screenFrom : { x: screenTo.x, y: screenTo.y },
      color,
      action: event.action,
      startTime: Date.now(),
      filePath: event.filePath,
      revealed: false,
    });

    // Highlight the file in the graph (only if already visible)
    if (event.action !== 'create') {
      fileGraph.highlightFile(event.filePath);
    }
  }

  reset(): void {
    this.particles = [];
  }

  draw(ctx: CanvasRenderingContext2D): void {
    const now = Date.now();
    const totalDuration = PARTICLE_DURATION + MATERIALIZE_DURATION;

    this.particles = this.particles.filter(
      (p) => now - p.startTime < totalDuration
    );

    for (const particle of this.particles) {
      const elapsed = now - particle.startTime;

      const isRead = particle.action === 'read';

      // Trigger file reveal when create particle arrives at destination
      if (particle.action === 'create' && !particle.revealed && elapsed >= PARTICLE_DURATION) {
        particle.revealed = true;
        if (this.fileGraph) {
          this.fileGraph.revealFile(particle.filePath);
          this.fileGraph.highlightFile(particle.filePath);
        }
      }

      if (elapsed < PARTICLE_DURATION) {
        // Particle traveling
        const t = elapsed / PARTICLE_DURATION;
        // Ease-out cubic
        const eased = 1 - Math.pow(1 - t, 3);
        const x = particle.from.x + (particle.to.x - particle.from.x) * eased;
        const y = particle.from.y + (particle.to.y - particle.from.y) * eased;

        if (isRead) {
          // Read: smaller, dimmer, dotted trail
          const trailLength = 3;
          for (let i = 0; i < trailLength; i++) {
            const tt = Math.max(0, eased - i * 0.05);
            const tx = particle.from.x + (particle.to.x - particle.from.x) * tt;
            const ty = particle.from.y + (particle.to.y - particle.from.y) * tt;
            ctx.beginPath();
            ctx.arc(tx, ty, 1.5, 0, Math.PI * 2);
            ctx.fillStyle = this.getColor(particle, 0.5 - i * 0.12);
            ctx.fill();
          }
          ctx.beginPath();
          ctx.arc(x, y, 2.5, 0, Math.PI * 2);
          ctx.fillStyle = this.getColor(particle, 0.7);
          ctx.shadowBlur = 6;
          ctx.shadowColor = particle.color;
          ctx.fill();
          ctx.shadowBlur = 0;
        } else {
          // Edit/Create: full trail
          const trailLength = 5;
          for (let i = 0; i < trailLength; i++) {
            const tt = Math.max(0, eased - i * 0.03);
            const tx = particle.from.x + (particle.to.x - particle.from.x) * tt;
            const ty = particle.from.y + (particle.to.y - particle.from.y) * tt;
            ctx.beginPath();
            ctx.arc(tx, ty, 3 - i * 0.5, 0, Math.PI * 2);
            ctx.fillStyle = this.getColor(particle, 0.8 - i * 0.15);
            ctx.fill();
          }
          ctx.beginPath();
          ctx.arc(x, y, 4, 0, Math.PI * 2);
          ctx.fillStyle = this.getColor(particle, 1);
          ctx.shadowBlur = 12;
          ctx.shadowColor = particle.color;
          ctx.fill();
          ctx.shadowBlur = 0;
        }
      } else if (particle.action === 'create') {
        // Materialize effect for new files - expanding ring at the file location
        const mt = (elapsed - PARTICLE_DURATION) / MATERIALIZE_DURATION;
        const scale = Math.min(1, mt * 1.2);
        const alpha = 1 - mt;

        ctx.save();
        ctx.globalAlpha = alpha;
        ctx.beginPath();
        ctx.arc(particle.to.x, particle.to.y, 8 * scale, 0, Math.PI * 2);
        ctx.strokeStyle = this.getColor(particle, 0.8);
        ctx.lineWidth = 2;
        ctx.stroke();
        ctx.restore();
      }
    }
  }

  private getColor(particle: ActiveParticle, alpha: number): string {
    const hex = particle.color;
    const r = parseInt(hex.slice(1, 3), 16);
    const g = parseInt(hex.slice(3, 5), 16);
    const b = parseInt(hex.slice(5, 7), 16);
    return `rgba(${r}, ${g}, ${b}, ${alpha})`;
  }
}
