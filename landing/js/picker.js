/* Hero model picker: a wheel of model names that turns to the one answering
   each request. Now and then the first choice is busy and the next one steps in. */
(() => {
  const picker = document.querySelector('[data-picker]');
  if (!picker) return;
  const list = picker.querySelector('[data-picker-list]');
  const prompt = picker.querySelector('[data-picker-prompt]');
  const time = picker.querySelector('[data-picker-time]');
  const status = picker.querySelector('[data-picker-status]');
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;

  const models = ['Gemma 4', 'Qwen3.6 27B', 'GPT-OSS 120B', 'DeepSeek V4.1 Flash', 'GLM 5.3 Flash', 'Muse Spark', 'Nemotron 3 Ultra'];
  const requests = [
    { text: '“Why does this test fail only on CI?”', model: 'DeepSeek V4.1 Flash', busy: 'GPT-OSS 120B', time: '0.7 s' },
    { text: '“Summarise my inbox”', model: 'Muse Spark', time: '0.4 s' },
    { text: '“Plan three days in Lisbon”', model: 'Gemma 4', time: '0.9 s' },
    { text: '“Translate this email to Spanish”', model: 'Qwen3.6 27B', busy: 'Nemotron 3 Ultra', time: '0.6 s' },
    { text: '“Write a regex for UK postcodes”', model: 'GLM 5.3 Flash', time: '0.5 s' },
  ];

  // Three copies so the wheel can always turn forward; it jumps back a copy unseen.
  const n = models.length;
  const rows = [...models, ...models, ...models].map((name) => {
    const li = document.createElement('li');
    li.textContent = name;
    list.appendChild(li);
    return li;
  });
  const CENTER = 3;
  let pos = n + models.indexOf(requests[0].model);

  const rowHeight = () => rows[0].getBoundingClientRect().height;
  const fades = [1, 0.34, 0.16, 0.06];

  function place(instant = false) {
    list.classList.toggle('instant', instant);
    list.style.transform = `translateY(${(CENTER - pos) * rowHeight()}px)`;
    rows.forEach((li, i) => {
      const d = Math.abs(i - pos);
      li.style.opacity = d < fades.length ? fades[d] : 0;
      li.classList.toggle('on', d === 0);
    });
    if (instant) void list.offsetHeight;
  }

  // The next copy of a model at or after the current position.
  const ahead = (name) => {
    let i = pos + 1;
    while (rows[i].textContent !== name) i += 1;
    return i;
  };

  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, reduced ? 0 : ms));

  function turnTo(name) {
    if (pos >= 2 * n) {
      // Jump back a copy unseen, so there is always room to turn forward.
      pos -= n;
      place(true);
    }
    pos = ahead(name);
    place();
  }

  async function loop() {
    let round = 0;
    for (;;) {
      const req = requests[round % requests.length];
      prompt.classList.add('out');
      time.classList.add('out');
      rows.forEach((li) => li.classList.remove('done', 'busy'));
      await wait(400);
      prompt.textContent = req.text;
      prompt.classList.remove('out');
      status.textContent = 'Choosing the best model…';
      await wait(500);

      if (req.busy) {
        turnTo(req.busy);
        await wait(1000);
        rows[pos].classList.add('busy');
        status.textContent = `${req.busy} is busy, switching…`;
        await wait(900);
      }
      turnTo(req.model);
      await wait(950);
      rows[pos].classList.add('done');
      time.textContent = req.time;
      time.classList.remove('out');
      status.textContent = `Answered by ${req.model}`;
      round += 1;
      await wait(3200);
    }
  }

  place(true);
  rows[pos].classList.add('done');
  new ResizeObserver(() => place(true)).observe(picker);
  setTimeout(loop, 2600);
})();
