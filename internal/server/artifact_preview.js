// OCTO-FORK: report only layout dimensions so fixed-size deliverables can fit
// their cross-origin preview without giving the parent document DOM access.
(() => {
  if (window.parent === window) return;
  let scheduled = false;
  let previous = '';
  function report() {
    scheduled = false;
    const root = document.documentElement;
    const body = document.body;
    if (!root || !body) return;
    const layout = {
      type: 'octo:artifact-layout',
      width: Math.max(root.scrollWidth, root.offsetWidth, body.scrollWidth, body.offsetWidth),
      height: Math.max(root.scrollHeight, root.offsetHeight, body.scrollHeight, body.offsetHeight),
      viewportWidth: window.innerWidth,
      // A clipped canvas (for example a poster) fits as a whole. Documents
      // with normal overflow retain vertical scrolling instead of shrinking.
      clipped: [root, body].some(el => ['hidden', 'clip'].includes(getComputedStyle(el).overflowY)),
    };
    const key = JSON.stringify(layout);
    if (key === previous) return;
    previous = key;
    // Only dimensions cross the frame boundary; no content, grant or API data.
    window.parent.postMessage(layout, '*');
  }
  function schedule() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(report);
  }
  const observer = new ResizeObserver(schedule);
  observer.observe(document.documentElement);
  if (document.body) observer.observe(document.body);
  window.addEventListener('resize', schedule);
  window.addEventListener('load', schedule);
  window.addEventListener('message', event => {
    if (event.source !== window.parent || event.data?.type !== 'octo:artifact-measure') return;
    previous = '';
    schedule();
  });
  schedule();
})();
