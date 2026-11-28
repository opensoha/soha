export const tableColumnPresets = {
  status: {
    width: 120,
  },
  datetime: {
    width: 180,
  },
  task: {
    width: 156,
  },
  action: {
    key: 'actions' as const,
    width: 64,
    align: 'center' as const,
    className: 'soha-table-actions-column',
    fixed: 'right' as const,
  },
}
