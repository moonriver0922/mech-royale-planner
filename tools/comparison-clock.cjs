// Comparison-only logical clock so paired matches spend the same work on the
// shared map cache per observation. The referee still uses real wall time.
let calls = 0;
Object.defineProperty(performance, "now", {
  value: () => (++calls) * 0.3
});
