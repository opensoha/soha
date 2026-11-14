import {
  createDefaultReleaseDagDefinition,
  normalizeReleaseDagDefinition,
  type ReleaseDagDefinition,
} from '@/components/release-flow-dag-definition'
import { ReleaseFlowDagEditor } from '@/components/release-flow-dag-editor'
import { useEffect, useState } from 'react'

export default function HealingDagEditor({
  disabled = false,
  initialDefinition,
  onChange,
}: {
  disabled?: boolean
  initialDefinition?: ReleaseDagDefinition
  onChange: (definition: ReleaseDagDefinition) => void
}) {
  const [definition, setDefinition] = useState(() =>
    normalizeReleaseDagDefinition(initialDefinition ?? createDefaultReleaseDagDefinition()),
  )

  useEffect(() => onChange(definition), [definition, onChange])

  return (
    <div
      ref={(element) => {
        if (element) element.inert = disabled
      }}
      aria-disabled={disabled}
    >
      <ReleaseFlowDagEditor
        initialDefinition={definition}
        onChange={(next) => setDefinition(next)}
      />
    </div>
  )
}
