import { ControllerDetailPage } from '../shared/controller-detail-page'

export function CronJobDetailPage() {
  return <ControllerDetailPage resource="cronjobs" title="CronJob" paramKey="cronJobName" />
}
