// Mobile IME password-composition settle helper.
//
// Mobile keyboards (Android Gboard, CJK IMEs) fire compositionend
// asynchronously after the user taps "done". Reading the password value
// before that event fires yields stale or partial text. settlePasswordInput
// blurs the field, waits for compositionend (or a 350ms timeout), then lets
// two animation frames + 80ms pass so the final text is committed before the
// caller reads the value.

function nextFrame() {
	return new Promise((resolve) => requestAnimationFrame(resolve));
}

function wait(ms) {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

// waitForPasswordComposition resolves when an in-progress IME composition
// ends, or immediately if none is active. The 350ms timeout guards against a
// compositionend that never fires.
function waitForPasswordComposition(el, isComposing) {
	if (!el || !isComposing()) return Promise.resolve(false);
	return new Promise((resolve) => {
		let settled = false;
		const done = (ended) => {
			if (settled) return;
			settled = true;
			el.removeEventListener("compositionend", onCompositionEnd);
			resolve(ended);
		};
		const onCompositionEnd = () => done(true);
		el.addEventListener("compositionend", onCompositionEnd, { once: true });
		setTimeout(() => done(false), 350);
	});
}

// settlePasswordInput lets mobile IMEs commit their final text before the
// caller reads the password value. Pass isComposing as a function returning
// the current composition state (tracked via compositionstart/compositionend
// listeners on the element). onDebug is an optional diagnostic callback that
// receives the settle-start and settle-done events.
export async function settlePasswordInput(el, isComposing, { onDebug } = {}) {
	if (!el) return;
	onDebug?.("password-input-settle-start", {
		active: document.activeElement === el,
		composing: isComposing(),
	});
	const compositionDone = waitForPasswordComposition(el, isComposing);
	if (document.activeElement === el) el.blur();
	await compositionDone;
	await nextFrame();
	await wait(80);
	await nextFrame();
	onDebug?.("password-input-settle-done");
}
