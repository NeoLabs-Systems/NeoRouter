/* Hero routing flow: lines from the apps to the hub and from the hub to the
   models, with a request travelling along the chosen route. */
(() => {
  const flow = document.querySelector('[data-flow]');
  if (!flow) return;
  const svgNS = 'http://www.w3.org/2000/svg';
  const stage = flow.querySelector('.flow-stage');
  const svg = flow.querySelector('[data-flow-lines]');
  const hub = flow.querySelector('[data-flow-hub] svg');
  const apps = [...flow.querySelectorAll('[data-flow-app]')];
  const models = [...flow.querySelectorAll('[data-flow-model]')];
  const status = flow.querySelector('[data-flow-status]');
  const statusText = flow.querySelector('[data-flow-text]');
  const modeEl = flow.querySelector('[data-flow-mode]');
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;

  const modes = ['Smart', 'Fast', 'Reliable', 'Free only'];
  const timings = ['0.4 s', '0.6 s', '0.9 s', '1.2 s', '0.5 s'];
  let inPaths = [];
  let outPaths = [];

  function anchor(el, side) {
    const s = stage.getBoundingClientRect();
    const r = el.getBoundingClientRect();
    const x = side === 'left' ? r.left - s.left : side === 'right' ? r.right - s.left : r.left - s.left + r.width / 2;
    return { x, y: r.top - s.top + r.height / 2 };
  }

  function curve(a, b) {
    const dx = (b.x - a.x) * 0.5;
    return `M${a.x},${a.y} C${a.x + dx},${a.y} ${b.x - dx},${b.y} ${b.x},${b.y}`;
  }

  function draw() {
    const s = stage.getBoundingClientRect();
    svg.setAttribute('viewBox', `0 0 ${s.width} ${s.height}`);
    svg.replaceChildren();
    const hubLeft = anchor(hub, 'left');
    const hubRight = anchor(hub, 'right');
    const make = (d) => {
      const path = document.createElementNS(svgNS, 'path');
      path.setAttribute('d', d);
      svg.appendChild(path);
      return path;
    };
    inPaths = apps.map((app) => make(curve(anchor(app, 'right'), hubLeft)));
    outPaths = models.map((model) => make(curve(hubRight, anchor(model, 'left'))));
  }

  function pulse(path, duration) {
    if (reduced || !path) return Promise.resolve();
    return new Promise((resolve) => {
      const dot = document.createElementNS(svgNS, 'circle');
      dot.setAttribute('r', '4');
      svg.appendChild(dot);
      const length = path.getTotalLength();
      const start = performance.now();
      const step = (now) => {
        const t = Math.min(1, (now - start) / duration);
        const eased = t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
        const p = path.getPointAtLength(eased * length);
        dot.setAttribute('cx', p.x);
        dot.setAttribute('cy', p.y);
        if (t < 1) requestAnimationFrame(step);
        else { dot.remove(); resolve(); }
      };
      requestAnimationFrame(step);
    });
  }

  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  const clear = () => {
    [...inPaths, ...outPaths].forEach((p) => p.classList.remove('on', 'fail'));
    [...apps, ...models].forEach((n) => n.classList.remove('on'));
  };
  const name = (model) => model.querySelector('b').textContent;
  const setStatus = (text, warn = false) => {
    statusText.textContent = text;
    status.classList.toggle('warn', warn);
  };

  let round = 0;
  async function loop() {
    for (;;) {
      clear();
      const appIndex = round % apps.length;
      if (round % 3 === 0) modeEl.textContent = `Mode: ${modes[(round / 3) % modes.length]}`;
      apps[appIndex].classList.add('on');
      inPaths[appIndex]?.classList.add('on');
      setStatus('Choosing the best model…');
      await pulse(inPaths[appIndex], 900);

      let target = (round * 3 + 1) % models.length;
      // Every few rounds the first choice is busy and the next one steps in.
      if (round % 4 === 2) {
        const busy = models[target];
        busy.classList.add('busy');
        outPaths[target]?.classList.add('fail');
        setStatus(`${name(busy)} is busy, switching…`, true);
        await wait(reduced ? 0 : 700);
        target = (target + 1) % models.length;
        setTimeout(() => {
          busy.classList.remove('busy');
          outPaths.forEach((p) => p.classList.remove('fail'));
        }, 2600);
      }
      models[target].classList.add('on');
      outPaths[target]?.classList.add('on');
      await pulse(outPaths[target], 900);
      setStatus(`Answered by ${name(models[target])} · free · ${timings[round % timings.length]}`);
      round += 1;
      await wait(1900);
    }
  }

  draw();
  new ResizeObserver(draw).observe(stage);
  loop();
})();
