"use client";

import { useRef, useState } from "react";

/** A card photo you can zoom with the mouse wheel and drag, like the desktop's editor. Double-click resets. */
export default function ZoomPhoto({ src, alt }: { src?: string | null; alt: string }) {
  const [z, setZ] = useState({ scale: 1, x: 0, y: 0 });
  const drag = useRef<{ x: number; y: number } | null>(null);
  const box = useRef<HTMLDivElement>(null);
  const [last, setLast] = useState(src);
  if (src !== last) {
    setLast(src);
    setZ({ scale: 1, x: 0, y: 0 });
  }

  function wheel(e: React.WheelEvent) {
    if (!src || !box.current) return;
    e.preventDefault();
    const r = box.current.getBoundingClientRect();
    const px = e.clientX - r.left - r.width / 2, py = e.clientY - r.top - r.height / 2;
    const next = Math.min(8, Math.max(1, z.scale * (e.deltaY < 0 ? 1.25 : 0.8)));
    const k = next / z.scale;
    setZ(next === 1 ? { scale: 1, x: 0, y: 0 } : { scale: next, x: px - (px - z.x) * k, y: py - (py - z.y) * k });
  }

  return (
    <div ref={box} onWheel={wheel} onDoubleClick={() => setZ({ scale: 1, x: 0, y: 0 })}
      onMouseDown={(e) => { if (z.scale > 1) drag.current = { x: e.clientX - z.x, y: e.clientY - z.y }; }}
      onMouseMove={(e) => { if (drag.current) setZ({ ...z, x: e.clientX - drag.current.x, y: e.clientY - drag.current.y }); }}
      onMouseUp={() => { drag.current = null; }} onMouseLeave={() => { drag.current = null; }}
      className={`relative flex h-full w-full items-center justify-center overflow-hidden rounded-lg bg-wash ${z.scale > 1 ? "cursor-move" : ""}`}>
      {src ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={src} alt={alt} draggable={false} className="max-h-full max-w-full select-none object-contain"
          style={{ transform: `translate(${z.x}px, ${z.y}px) scale(${z.scale})`, transformOrigin: "center" }} />
      ) : (
        <span className="text-[13px] text-muted">No photo</span>
      )}
    </div>
  );
}
