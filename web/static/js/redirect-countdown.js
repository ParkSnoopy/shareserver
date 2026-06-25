const counter = document.querySelector("[data-redirect-countdown]");

if (counter) {
	const target = counter.dataset.redirectTo || "/";
	let seconds = Number(counter.dataset.seconds || counter.textContent || 5);
	const tick = () => {
		counter.textContent = String(Math.max(0, seconds));
		if (seconds <= 0) {
			window.location.assign(target);
			return;
		}
		seconds -= 1;
		setTimeout(tick, 1000);
	};
	tick();
}
