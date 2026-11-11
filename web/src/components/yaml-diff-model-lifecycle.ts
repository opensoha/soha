interface OwnedDiffModels {
  original: { dispose: () => void }
  modified: { dispose: () => void }
}

/** Detach before disposing: Monaco observes model disposal while the widget is still mounted. */
export function releaseOwnedDiffModels(editor: {
  getModel: () => OwnedDiffModels | null
  setModel: (model: null) => void
}) {
  const models = editor.getModel()
  editor.setModel(null)
  models?.original.dispose()
  models?.modified.dispose()
}
