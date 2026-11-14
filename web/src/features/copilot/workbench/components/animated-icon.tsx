import Icon from '@ant-design/icons'
import type { ReactNode } from 'react'

type AssistantIconName =
  | 'chat'
  | 'search'
  | 'tasks'
  | 'results'
  | 'calendar'
  | 'identity'
  | 'settings'
  | 'workbench'

const shapes: Record<AssistantIconName, ReactNode> = {
  chat: (
    <>
      <path
        className="soha-ai-icon-draw"
        pathLength="1"
        d="M21 11a8 8 0 0 1-8 8H6l-3 3v-7a8 8 0 0 1-1-4 8 8 0 0 1 8-8h3a8 8 0 0 1 8 8Z"
      />
      {[7, 11, 15].map((x, index) => (
        <circle
          key={x}
          className="soha-ai-icon-dot"
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
      <circle className="soha-ai-icon-draw" pathLength="1" cx="10" cy="10" r="6.5" />
      <path className="soha-ai-icon-handle" d="m15 15 5.5 5.5" />
      <path className="soha-ai-icon-scan" pathLength="1" d="M6.5 10h7" />
    </>
  ),
  tasks: (
    <>
      {[6, 12, 18].map((y, index) => (
        <g key={y} data-step={index}>
          <path className="soha-ai-icon-check" pathLength="1" d={`m3 ${y} 2 2 3-4`} />
          <path className="soha-ai-icon-draw" pathLength="1" d={`M12 ${y}h8`} />
        </g>
      ))}
    </>
  ),
  results: (
    <>
      <path className="soha-ai-icon-draw" pathLength="1" d="M13.5 3H5v18h14V8.5L13.5 3Z" />
      <path className="soha-ai-icon-fold" d="M13.5 3v5.5H19" />
      {[12, 15, 18].map((y, index) => (
        <path
          key={y}
          data-step={index}
          className="soha-ai-icon-write"
          pathLength="1"
          d={`M8 ${y}h${index === 2 ? 5 : 8}`}
        />
      ))}
    </>
  ),
  calendar: (
    <>
      <path className="soha-ai-icon-draw" pathLength="1" d="M4 5h16v16H4V5Zm0 5h16M8 3v4m8-4v4" />
      <g className="soha-ai-icon-page">
        {[
          { x: 8, y: 13 },
          { x: 14, y: 13 },
          { x: 8, y: 17 },
          { x: 14, y: 17 },
        ].map(({ x, y }, index) => (
          <rect
            key={index}
            className="soha-ai-icon-cell"
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
      <g className="soha-ai-icon-face">
        <circle cx="12" cy="7.5" r="4" />
        <g className="soha-ai-icon-eyes" fill="currentColor" stroke="none">
          <circle cx="10.6" cy="7.5" r="0.45" />
          <circle cx="13.4" cy="7.5" r="0.45" />
        </g>
      </g>
      <path className="soha-ai-icon-draw" pathLength="1" d="M4.5 21v-2a7.5 7.5 0 0 1 15 0v2" />
    </>
  ),
  settings: (
    <>
      <path
        className="soha-ai-icon-draw"
        pathLength="1"
        d="m9.5 3-.6 2-1.5.9-2-.5-2.5 4.3 1.5 1.5v1.8l-1.5 1.5 2.5 4.3 2-.5 1.5.9.6 2h5l.6-2 1.5-.9 2 .5 2.5-4.3-1.5-1.5v-1.8l1.5-1.5-2.5-4.3-2 .5-1.5-.9-.6-2h-5Z"
      />
      <circle className="soha-ai-icon-knob" cx="12" cy="12" r="3" />
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
          className="soha-ai-icon-tile"
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

export function AnimatedAssistantIcon({ name }: { name: AssistantIconName }) {
  return (
    <Icon className="soha-ai-animated-icon" data-icon={name} viewBox="0 0 24 24" aria-hidden="true">
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
