export interface SplitResizeEvent {
  deltaX: number;
  startX: number;
  currentX: number;
  event: KeyboardEvent | MouseEvent;
}
