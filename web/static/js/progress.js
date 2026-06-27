// fmtBytes renders byte counts in compact binary units for terminal UI text.
export function fmtBytes(n) {
	if (!n) return "0 B";
	const u = ["B", "KiB", "MiB", "GiB"];
	let i = 0;
	let x = n;
	while (x >= 1024 && i < u.length - 1) {
		x /= 1024;
		i++;
	}
	return `${x.toFixed(i ? 1 : 0)} ${u[i]}`;
}

// Progress renders ordered transfer phases with throttled updates and failure state.
export class Progress {
	// constructor binds progress rendering to one DOM element.
	constructor(el) {
		this.el = el;
		this.reset();
	}

	// reset clears visible lines and stops delayed or pulsing updates.
	reset() {
		if (this.timer) clearTimeout(this.timer);
		if (this._pulseTimers) for (const t of this._pulseTimers) clearInterval(t);
		this.order = [];
		this.seen = new Set();
		this.failed = new Set();
		this.lines = new Map();
		this.pending = null;
		this.timer = null;
		this._pulseTimers = new Set();
		this.pulses = new Map();
		this.t0 = performance.now();
		this.el.textContent = "";
	}

	// bar renders a fixed-width text progress bar.
	bar(p) {
		const w = 20;
		const n = Math.max(0, Math.min(w, Math.round(p * w)));
		return `[${"#".repeat(n)}${" ".repeat(w - n)}]`;
	}

	// render writes phase lines in first-seen order.
	render() {
		this.el.textContent = this.order
			.map((phase) => this.lines.get(phase))
			.join("\n");
	}

	// line formats one transfer phase with percent, bytes, and state.
	line(phase, done, total, state) {
		const p = total ? done / total : 0;
		return `${phase.padEnd(9)} ${this.bar(p)} ${String(Math.round(p * 100)).padStart(3)}% ${fmtBytes(done)} / ${fmtBytes(total)} ${state}`;
	}

	// cancelPending removes a delayed update for a completed or failed phase.
	cancelPending(phase) {
		if (this.pending?.phase === phase) {
			this.pending = null;
			if (this.timer) {
				clearTimeout(this.timer);
				this.timer = null;
			}
		}
	}

	// set queues a throttled phase update so fast operations do not flicker.
	set(phase, done, total, state = "") {
		if (this.failed.has(phase)) return;
		if (!this.seen.has(phase)) {
			this.seen.add(phase);
			this.order.push(phase);
		}
		this.pending = { phase, done, total, state };
		if (this.timer) return;
		this.timer = setTimeout(() => this.flush(), 500);
	}

	// flush commits the latest queued update and estimates speed when no state is given.
	flush() {
		this.timer = null;
		if (!this.pending) return;
		const { phase, done, total, state } = this.pending;
		this.pending = null;
		if (this.failed.has(phase)) return;
		const dt = Math.max(0.1, (performance.now() - this.t0) / 1000);
		const speed = done ? `${fmtBytes(done / dt)}/s` : state;
		this.lines.set(phase, this.line(phase, done, total, state || speed));
		this.render();
	}

	// fail freezes a phase in error state and cancels pending success updates.
	fail(phase, msg, done = 0, total = 0) {
		this.failed.add(phase);
		this.cancelPending(phase);
		if (this.timer) {
			clearTimeout(this.timer);
			this.timer = null;
		}
		this.pending = null;
		if (!this.seen.has(phase)) {
			this.seen.add(phase);
			this.order.push(phase);
		}
		if (total)
			this.lines.set(phase, this.line(phase, done, total, `failed: ${msg}`));
		else
			this.lines.set(phase, `${phase.padEnd(9)} ${this.bar(0)} failed: ${msg}`);
		this.render();
	}

	// pulse shows bounded synthetic progress for work without byte callbacks.
	pulse(phase, total, state = "working") {
		if (!this.seen.has(phase)) {
			this.seen.add(phase);
			this.order.push(phase);
		}
		const start = performance.now();
		this.pulses.set(phase, { total, start, state });
		const tick = () => {
			if (this.failed.has(phase)) return;
			const meta = this.pulses.get(phase);
			if (!meta) return;
			const elapsed = (performance.now() - meta.start) / 1000;
			const p = Math.min(0.95, elapsed / 8);
			this.set(phase, Math.round(meta.total * p), meta.total, meta.state);
		};
		tick();
		const timer = setInterval(tick, 500);
		this._pulseTimers.add(timer);
		return () => {
			clearInterval(timer);
			this._pulseTimers.delete(timer);
			this.pulses.delete(phase);
		};
	}

	// state relabels an active pulse and re-renders immediately so a mode
	// change (e.g. native -> pure-JS decrypt fallback) shows without waiting
	// for the next pulse tick. No-op when the phase is not pulsing or has failed.
	state(phase, newState) {
		if (this.failed.has(phase)) return;
		const meta = this.pulses.get(phase);
		if (!meta) return;
		meta.state = newState;
		const elapsed = (performance.now() - meta.start) / 1000;
		const p = Math.min(0.95, elapsed / 8);
		this.cancelPending(phase);
		this.lines.set(phase, this.line(phase, Math.round(meta.total * p), meta.total, newState));
		this.render();
	}

	// done marks a phase complete and prevents stale queued updates from overwriting it.
	done(phase, total) {
		if (this.failed.has(phase)) return;
		this.cancelPending(phase);
		if (this.timer) {
			clearTimeout(this.timer);
			this.timer = null;
		}
		this.pending = null;
		if (!this.seen.has(phase)) {
			this.seen.add(phase);
			this.order.push(phase);
		}
		this.lines.set(phase, this.line(phase, total, total, "done"));
		this.render();
	}
}
