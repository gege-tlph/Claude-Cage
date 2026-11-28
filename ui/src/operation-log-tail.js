import { useLayoutEffect, useRef, useState } from "react";
import { LOG_TAIL_STEP, eventKey, tailStart } from "./log-tail.mjs";

// useLogTail draws only the newest events of the advanced view, and older
// ones in steps as the view nears its top: drawing all 3000 events at once
// held the window for a moment when 高级 opened. At the end the drawn lines
// follow new events; scrolled up, the first drawn line stays put.
export function useLogTail(events, body) {
  const [first, setFirst] = useState(null);
  const height = useRef(null);
  const list = events || [];
  const start = tailStart(list, first);

  // Lines added above keep the lines in view where they were.
  useLayoutEffect(() => {
    const element = body.current;
    if (element && height.current !== null) element.scrollTop += element.scrollHeight - height.current;
    height.current = null;
  }, [first, body]);

  // scrolled follows the view: atEnd is whether it sits at its end.
  const scrolled = (atEnd) => {
    const element = body.current;
    if (!element || !list.length) return;
    if (atEnd) {
      if (first !== null) setFirst(null);
    } else if (element.scrollTop < 160 && start > 0) {
      height.current = element.scrollHeight;
      setFirst(eventKey(list[Math.max(0, start - LOG_TAIL_STEP)]));
    } else if (first === null) {
      setFirst(eventKey(list[start]));
    }
  };
  return { shown: start ? list.slice(start) : list, offset: start, scrolled, reset: () => setFirst(null) };
}
