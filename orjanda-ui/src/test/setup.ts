import '@testing-library/jest-dom/vitest';

// jsdom does not implement scrollIntoView; stub it for the chat autoscroll.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}
