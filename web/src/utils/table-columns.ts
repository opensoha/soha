export const tableColumnPresets = {
  status: {
    width: 120,
  },
  datetime: {
    width: 180,
  },
  task: {
    width: 160,
    fixed: 'right' as const,
  },
  action: {
    width: 140,
    align: 'center' as const,
    className: 'soha-table-actions-column',
    fixed: 'right' as const,
  },
}
