// Narrowing the campaign list in the browser. The list holds at most the
// last 100 campaigns, so there is nothing to ask the backend for.
import type { CampaignState, CampaignSummary } from '../api/types'

/** Turkish-aware lower case, so "İSTANBUL" finds "İstanbul" and "ISPARTA" finds "Isparta". */
function fold(text: string): string {
  return text.toLocaleLowerCase('tr')
}

/** Campaigns in the given state ("" for any) whose brief contains the query. */
export function filterCampaigns(list: CampaignSummary[], state: CampaignState | '', query: string): CampaignSummary[] {
  const q = fold(query.trim())
  return list.filter((c) => {
    if (state && c.state !== state) return false
    if (!q) return true
    return fold(c.brief).includes(q)
  })
}

/** How many campaigns are in each state. */
export function countByState(list: CampaignSummary[]): Record<CampaignState, number> {
  const counts: Record<CampaignState, number> = { scheduled: 0, sending: 0, done: 0, cancelled: 0 }
  for (const c of list) counts[c.state]++
  return counts
}
