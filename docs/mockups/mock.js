// Mockup harness: switch between the three screens with the pill at bottom-right
// or the keys 1 / 2 / 3. Not part of the product.
(function () {
  const screens = Array.from(document.querySelectorAll("section.screen"));
  const pills = Array.from(document.querySelectorAll(".harness a[data-screen]"));
  function show(name) {
    screens.forEach((s) => s.classList.toggle("on", s.dataset.screen === name));
    pills.forEach((p) => p.classList.toggle("on", p.dataset.screen === name));
    history.replaceState(null, "", "#" + name);
    window.scrollTo(0, 0);
  }
  pills.forEach((p) => p.addEventListener("click", () => show(p.dataset.screen)));
  document.addEventListener("keydown", (e) => {
    if (e.target.tagName === "INPUT") return;
    const i = Number(e.key) - 1;
    if (pills[i]) show(pills[i].dataset.screen);
  });
  const initial = location.hash.slice(1);
  show(pills.some((p) => p.dataset.screen === initial) ? initial : pills[0].dataset.screen);
})();
