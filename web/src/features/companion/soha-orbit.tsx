import type { CompanionInteractionMotion } from './motion'
import type { CompanionVisualState } from './types'

interface SohaOrbitProps {
  interaction?: CompanionInteractionMotion
  state: CompanionVisualState
  stateAnimation?: string
}

export function SohaOrbit({ interaction, state, stateAnimation }: SohaOrbitProps) {
  return (
    <svg
      aria-hidden="true"
      className="soha-companion-orbit"
      data-interaction={interaction?.animation}
      data-motion={stateAnimation}
      data-state={state}
      viewBox="0 0 180 190"
      xmlns="http://www.w3.org/2000/svg"
    >
      <ellipse className="soha-companion-orbit__shadow" cx="90" cy="174" rx="47" ry="8" />
      <g key={interaction?.sequence} className="soha-companion-orbit__body">
        <path className="soha-companion-orbit__antenna" d="M90 38V22" />
        <circle className="soha-companion-orbit__signal" cx="90" cy="17" r="7" />
        <g
          className="soha-companion-orbit__head-hit"
          data-companion-interaction="pet"
          data-hit-area="head"
        >
          <path className="soha-companion-orbit__ear" d="M49 67 30 48l4 42Z" />
          <path className="soha-companion-orbit__ear" d="m131 67 19-19-4 42Z" />
          <rect
            className="soha-companion-orbit__head"
            x="39"
            y="47"
            width="102"
            height="91"
            rx="38"
          />
          <path
            className="soha-companion-orbit__face"
            d="M57 79c20-18 46-18 66 0v28c-20 17-46 17-66 0Z"
          />
          <g className="soha-companion-orbit__eyes">
            <path d="M70 91v8" />
            <path d="M110 91v8" />
          </g>
          <path className="soha-companion-orbit__mouth" d="M82 111c5 4 11 4 16 0" />
        </g>
        <path className="soha-companion-orbit__chest" d="M61 136c4 24 54 24 58 0" />
        <circle className="soha-companion-orbit__core" cx="90" cy="148" r="8" />
      </g>
    </svg>
  )
}
