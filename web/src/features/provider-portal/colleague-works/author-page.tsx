import { useParams } from 'react-router-dom'
import { ColleagueWorksView } from './colleague-works-view'

export function PortalColleagueAuthorPage() {
  const { authorId = '' } = useParams()
  return <ColleagueWorksView authorId={authorId} />
}
