import Icon from '@ant-design/icons'
import type { ReactNode } from 'react'
import './animated-icon.css'

type HeyaIconName =
  | 'chat'
  | 'search'
  | 'tasks'
  | 'results'
  | 'calendar'
  | 'identity'
  | 'settings'
  | 'workbench'

const heyaShapes: Record<HeyaIconName, ReactNode> = {
  chat: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="M21 11a8 8 0 0 1-8 8H6l-3 3v-7a8 8 0 0 1-1-4 8 8 0 0 1 8-8h3a8 8 0 0 1 8 8Z"
      />
      {[7, 11, 15].map((x, index) => (
        <circle
          key={x}
          className="soha-icon-dot"
          data-step={index}
          cx={x}
          cy="11"
          r="1"
          fill="currentColor"
          stroke="none"
        />
      ))}
    </>
  ),
  search: (
    <>
      <circle className="soha-icon-draw" pathLength="1" cx="10" cy="10" r="6.5" />
      <path className="soha-icon-handle" d="m15 15 5.5 5.5" />
      <path className="soha-icon-scan" pathLength="1" d="M6.5 10h7" />
    </>
  ),
  tasks: (
    <>
      {[6, 12, 18].map((y, index) => (
        <g key={y} data-step={index}>
          <path className="soha-icon-check" pathLength="1" d={`m3 ${y} 2 2 3-4`} />
          <path className="soha-icon-draw" pathLength="1" d={`M12 ${y}h8`} />
        </g>
      ))}
    </>
  ),
  results: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M13.5 3H5v18h14V8.5L13.5 3Z" />
      <path className="soha-icon-fold" d="M13.5 3v5.5H19" />
      {[12, 15, 18].map((y, index) => (
        <path
          key={y}
          data-step={index}
          className="soha-icon-write"
          pathLength="1"
          d={`M8 ${y}h${index === 2 ? 5 : 8}`}
        />
      ))}
    </>
  ),
  calendar: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M4 5h16v16H4V5Zm0 5h16M8 3v4m8-4v4" />
      <g className="soha-icon-page">
        {[
          { x: 8, y: 13 },
          { x: 14, y: 13 },
          { x: 8, y: 17 },
          { x: 14, y: 17 },
        ].map(({ x, y }, index) => (
          <rect
            key={index}
            className="soha-icon-cell"
            data-step={index}
            x={x}
            y={y}
            width="2"
            height="2"
            rx="0.5"
            fill="currentColor"
            stroke="none"
          />
        ))}
      </g>
    </>
  ),
  identity: (
    <>
      <g className="soha-icon-face">
        <circle cx="12" cy="7.5" r="4" />
        <g className="soha-icon-eyes" fill="currentColor" stroke="none">
          <circle cx="10.6" cy="7.5" r="0.45" />
          <circle cx="13.4" cy="7.5" r="0.45" />
        </g>
      </g>
      <path className="soha-icon-draw" pathLength="1" d="M4.5 21v-2a7.5 7.5 0 0 1 15 0v2" />
    </>
  ),
  settings: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="m9.5 3-.6 2-1.5.9-2-.5-2.5 4.3 1.5 1.5v1.8l-1.5 1.5 2.5 4.3 2-.5 1.5.9.6 2h5l.6-2 1.5-.9 2 .5 2.5-4.3-1.5-1.5v-1.8l1.5-1.5-2.5-4.3-2 .5-1.5-.9-.6-2h-5Z"
      />
      <circle className="soha-icon-knob" cx="12" cy="12" r="3" />
    </>
  ),
  workbench: (
    <>
      {[
        { x: 3, y: 3 },
        { x: 14, y: 3 },
        { x: 3, y: 14 },
        { x: 14, y: 14 },
      ].map(({ x, y }, index) => (
        <rect
          key={index}
          className="soha-icon-tile"
          data-step={index}
          x={x}
          y={y}
          width="7"
          height="7"
          rx="1.3"
        />
      ))}
    </>
  ),
}

const menuShapes = {
  home: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="m3 10 9-7 9 7v11h-7v-7h-4v7H3Z" />
    </>
  ),
  gauge: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="M4 19a10 10 0 1 1 16 0M5 12h2m10 0h2M7 6l1 2m8-2-1 2"
      />
      <path className="soha-icon-write" pathLength="1" d="m12 14 4-4" />
      <circle cx="12" cy="14" r="1.5" />
    </>
  ),
  server: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M3 3h18v8H3Zm0 10h18v8H3Z" />
      <path className="soha-icon-write" pathLength="1" d="M12 7h5m-5 10h5" />
      <circle className="soha-icon-dot" cx="7" cy="7" r=".8" />
      <circle className="soha-icon-dot" data-step="1" cx="7" cy="17" r=".8" />
    </>
  ),
  desktop: (
    <>
      <rect className="soha-icon-draw" pathLength="1" x="3" y="3" width="18" height="13" rx="1.5" />
      <path className="soha-icon-write" pathLength="1" d="M12 16v5m-5 0h10" />
    </>
  ),
  cluster: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M12 8v5M5 16v-3h14v3" />
      <rect className="soha-icon-tile" x="9" y="2" width="6" height="6" rx="1" />
      <rect className="soha-icon-tile" data-step="1" x="2" y="16" width="6" height="6" rx="1" />
      <rect className="soha-icon-tile" data-step="2" x="16" y="16" width="6" height="6" rx="1" />
    </>
  ),
  puzzle: (
    <path
      className="soha-icon-draw"
      pathLength="1"
      d="M4 4h6V3a2 2 0 0 1 4 0v1h6v6h1a2 2 0 0 1 0 4h-1v6h-6v-1a2 2 0 0 0-4 0v1H4v-6h1a2 2 0 0 0 0-4H4Z"
    />
  ),
  shield: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="m12 2 9 4v6c0 5-6 9-9 10-3-1-9-5-9-10V6Z"
      />
      <path className="soha-icon-check" pathLength="1" d="m8 12 3 3 5-6" />
    </>
  ),
  boxes: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="m12 2 5 3v6l-5 3-5-3V5Zm0 6 5-3m-5 3L7 5m5 3v6M7 11l-5 3v6l5 3 5-3 5 3 5-3v-6l-5-3"
      />
      <path
        className="soha-icon-write"
        pathLength="1"
        d="m2 14 5 3 5-3 5 3 5-3M7 17v6m5-9v6m5-3v6"
      />
    </>
  ),
  network: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="m9 7-4 9m10-9 4 9M8 19h8" />
      <circle className="soha-icon-tile" cx="12" cy="5" r="3" />
      <circle className="soha-icon-tile" data-step="1" cx="5" cy="19" r="3" />
      <circle className="soha-icon-tile" data-step="2" cx="19" cy="19" r="3" />
    </>
  ),
  waves: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M2 6c4-6 6 6 10 0s6 6 10 0" />
      <path
        className="soha-icon-draw"
        data-step="1"
        pathLength="1"
        d="M2 12c4-6 6 6 10 0s6 6 10 0"
      />
      <path
        className="soha-icon-draw"
        data-step="2"
        pathLength="1"
        d="M2 18c4-6 6 6 10 0s6 6 10 0"
      />
    </>
  ),
  globe: (
    <>
      <circle className="soha-icon-draw" pathLength="1" cx="12" cy="12" r="10" />
      <path
        className="soha-icon-write"
        pathLength="1"
        d="M2 12h20M12 2c6 5 6 15 0 20-6-5-6-15 0-20Z"
      />
    </>
  ),
  bell: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M5 9a7 7 0 0 1 14 0v6l2 3H3l2-3ZM9 21h6" />
      <path className="soha-icon-write" pathLength="1" d="M12 2V1" />
    </>
  ),
  users: (
    <>
      <circle className="soha-icon-tile" cx="9" cy="7" r="4" />
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="M2 21v-2a7 7 0 0 1 14 0v2M16 3a4 4 0 0 1 0 8m3 3a6 6 0 0 1 3 5v2"
      />
    </>
  ),
  bot: (
    <>
      <rect className="soha-icon-draw" pathLength="1" x="4" y="6" width="16" height="15" rx="3" />
      <path d="M12 6V2m-2 0h4M1 12v4m22-4v4" />
      <g className="soha-icon-eyes">
        <circle cx="8" cy="12" r="1" />
        <circle cx="16" cy="12" r="1" />
      </g>
      <path className="soha-icon-write" pathLength="1" d="M8 17h8" />
    </>
  ),
  image: (
    <>
      <rect className="soha-icon-draw" pathLength="1" x="3" y="3" width="18" height="18" rx="2" />
      <circle className="soha-icon-dot" cx="8" cy="8" r="1.5" />
      <path className="soha-icon-write" pathLength="1" d="m3 17 5-5 4 4 4-6 5 7" />
    </>
  ),
  blocks: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="m12 2 9 5v10l-9 5-9-5V7Zm0 10 9-5m-9 5L3 7m9 5v10"
      />
      <path className="soha-icon-write" pathLength="1" d="m7.5 4.5 9 5v4" />
    </>
  ),
  flavor: (
    <>
      <rect className="soha-icon-draw" pathLength="1" x="5" y="5" width="14" height="14" rx="2" />
      <rect className="soha-icon-tile" x="9" y="9" width="6" height="6" rx="1" />
      <path
        className="soha-icon-write"
        pathLength="1"
        d="M9 2v3m6-3v3M9 19v3m6-3v3M2 9h3m-3 6h3m14-6h3m-3 6h3"
      />
    </>
  ),
  code: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="m7 6-5 6 5 6m10-12 5 6-5 6" />
      <path className="soha-icon-write" pathLength="1" d="m14 3-4 18" />
    </>
  ),
  activity: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M7 12h10M12 12V5h5" />
      <rect className="soha-icon-tile" x="1" y="9" width="6" height="6" rx="1.5" />
      <rect className="soha-icon-tile" data-step="1" x="17" y="2" width="6" height="6" rx="1.5" />
      <rect className="soha-icon-tile" data-step="2" x="17" y="9" width="6" height="6" rx="1.5" />
    </>
  ),
  history: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M3 10a9 9 0 1 1 1 7M3 4v6h6" />
      <path className="soha-icon-write" pathLength="1" d="M12 7v5l3 2" />
    </>
  ),
  info: (
    <>
      <circle className="soha-icon-draw" pathLength="1" cx="12" cy="12" r="10" />
      <path className="soha-icon-write" pathLength="1" d="M11 11h1v6m-1 0h2" />
      <circle className="soha-icon-dot" cx="12" cy="7" r=".7" />
    </>
  ),
  sync: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="M3 10a9 9 0 0 1 16-5l2 3m0-6v6h-6M21 14a9 9 0 0 1-16 5l-2-3m0 6v-6h6"
      />
    </>
  ),
  'panels-top-left': (
    <>
      <rect className="soha-icon-draw" pathLength="1" x="2" y="3" width="20" height="18" rx="2" />
      <path className="soha-icon-write" pathLength="1" d="M2 8h20M8 8v13" />
    </>
  ),
  megaphone: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="m3 9 17-6v16L3 13Zm2 5 2 7h4l-3-6" />
      <path className="soha-icon-write" pathLength="1" d="M16 5v12M23 9v4" />
    </>
  ),
  'menu-square': (
    <>
      <rect className="soha-icon-draw" pathLength="1" x="3" y="3" width="18" height="18" rx="2" />
      <path className="soha-icon-write" pathLength="1" d="M7 8h10m-10 4h10m-10 4h10" />
    </>
  ),
  'file-clock': (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="M10 21H4V3h10l5 5v3M14 3v5h5M7 8h3m-3 4h3m-3 4h2"
      />
      <circle className="soha-icon-draw" pathLength="1" cx="17" cy="17" r="5" />
      <path className="soha-icon-write" pathLength="1" d="M17 14v3l2 1" />
    </>
  ),
  siren: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M6 18v-6a6 6 0 0 1 12 0v6M3 18h18v4H3Z" />
      <path className="soha-icon-write" pathLength="1" d="M12 1v2M3 5l2 2m14 0 2-2M1 12h2m18 0h2" />
    </>
  ),
  docker: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="M2 13h15c2 0 3-1 3-3l2 2c0 7-5 10-11 10-5 0-9-3-9-9Z"
      />
      <path className="soha-icon-write" pathLength="1" d="M4 13V9h4v4m0-4V5h4v8m0-4h4v4M8 9h4" />
      <circle className="soha-icon-dot" cx="6" cy="17" r=".6" />
    </>
  ),
  book: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="M12 5C9 2 5 2 2 3v17c4-1 7-1 10 2 3-3 6-3 10-2V3c-3-1-7-1-10 2Zm0 0v17"
      />
      <path className="soha-icon-write" pathLength="1" d="M5 7h4m6 0h4M5 11h4m6 0h4" />
    </>
  ),
  inspect: (
    <>
      <rect className="soha-icon-draw" pathLength="1" x="2" y="2" width="17" height="17" rx="2" />
      <path className="soha-icon-write" pathLength="1" d="M5 6h9M5 10h3m-3 4h3" />
      <circle className="soha-icon-draw" pathLength="1" cx="15" cy="15" r="4" />
      <path className="soha-icon-handle" d="m18 18 4 4" />
    </>
  ),
  key: (
    <>
      <path className="soha-icon-draw" pathLength="1" d="M14 10a6 6 0 1 0-4 4l3 3h3v3h4v-4Z" />
      <circle className="soha-icon-dot" cx="7" cy="7" r="1" />
    </>
  ),
  link: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="m10 6 3-3a5 5 0 0 1 8 8l-3 3m-4 4-3 3a5 5 0 0 1-8-8l3-3"
      />
      <path className="soha-icon-write" pathLength="1" d="m8 16 8-8" />
    </>
  ),
  palette: (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="M12 2a10 10 0 1 0 0 20h1a3 3 0 0 0 2-5c-1-1 0-3 2-3h2a3 3 0 0 0 3-3c0-5-5-9-10-9Z"
      />
      {[
        [7, 8],
        [12, 6],
        [17, 8],
        [6, 13],
      ].map(([x, y], i) => (
        <circle key={i} className="soha-icon-dot" data-step={i} cx={x} cy={y} r="1" />
      ))}
    </>
  ),
  'radio-tower': (
    <>
      <path
        className="soha-icon-draw"
        pathLength="1"
        d="m12 9-5 13m5-13 5 13M9 17h6M5 3a9 9 0 0 0 0 12M19 3a9 9 0 0 1 0 12"
      />
      <path className="soha-icon-write" pathLength="1" d="M8 6a4 4 0 0 0 0 6m8-6a4 4 0 0 1 0 6" />
      <circle className="soha-icon-dot" cx="12" cy="9" r="1" />
    </>
  ),
  storage: (
    <>
      <ellipse className="soha-icon-draw" pathLength="1" cx="12" cy="5" rx="9" ry="3" />
      <path className="soha-icon-draw" pathLength="1" d="M3 5v14c0 4 18 4 18 0V5" />
      <path className="soha-icon-write" pathLength="1" d="M3 12c0 4 18 4 18 0" />
    </>
  ),
  wrench: (
    <path
      className="soha-icon-draw"
      pathLength="1"
      d="M21 3a6 6 0 0 1-8 8L5 21a2 2 0 0 1-3-3l10-8a6 6 0 0 1 8-8l-4 4 2 2Z"
    />
  ),
} satisfies Record<string, ReactNode>

const shapes = {
  ...heyaShapes,
  ...menuShapes,
  cog: heyaShapes.settings,
  user: heyaShapes.identity,
  'clipboard-list': heyaShapes.tasks,
}

export type AnimatedIconName = keyof typeof shapes

export function AnimatedIcon({ name }: { name: AnimatedIconName }) {
  return (
    <Icon className="soha-animated-icon" data-icon={name} viewBox="0 0 24 24" aria-hidden="true">
      <g
        fill="none"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        {shapes[name]}
      </g>
    </Icon>
  )
}
