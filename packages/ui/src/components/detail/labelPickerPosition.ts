const EDGE_GAP = 12;
const TRIGGER_GAP = 4;
const MAX_WIDTH = 360;
const MIN_WIDTH = 280;

export function labelPickerPopoverStyle(trigger: DOMRect, viewportWidth: number): string {
  const availableWidth = Math.max(0, viewportWidth - EDGE_GAP * 2);
  const width = Math.min(MAX_WIDTH, availableWidth);
  const narrow = availableWidth <= MIN_WIDTH;
  const left = narrow
    ? EDGE_GAP
    : Math.min(
      Math.max(EDGE_GAP, trigger.right - width),
      Math.max(EDGE_GAP, viewportWidth - width - EDGE_GAP),
    );

  return [
    `left: ${Math.round(left)}px`,
    `top: ${Math.round(trigger.bottom + TRIGGER_GAP)}px`,
    `width: ${Math.round(width)}px`,
  ].join("; ");
}
