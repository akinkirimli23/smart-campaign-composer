import { describe, expect, it } from 'vitest'
import { countByState, filterCampaigns } from './campaigns'
import type { CampaignState, CampaignSummary } from '../api/types'

function campaign(id: number, state: CampaignState, brief: string): CampaignSummary {
  const messages = { total: 10, pending: 0, sending: 0, sent: 10, failed: 0, skipped: 0, cancelled: 0 }
  return { id, brief, state, created_at: 'now', send_at: 'now', messages }
}

const list = [
  campaign(19, 'done', "İstanbul ve Ankara'da 1000 TL üzeri harcayanları ön satışa davet et."),
  campaign(14, 'scheduled', 'Son 7 günde kayıt olanlara bir hafta sonra %10 indirim hatırlatması gönder.'),
  campaign(12, 'cancelled', 'Isparta ve çevresine gül sezonu kampanyası.'),
  campaign(4, 'sending', ''),
]

describe('campaign list filters', () => {
  it('keeps everything without a filter', () => {
    expect(filterCampaigns(list, '', '  ')).toEqual(list)
  })

  it('narrows by state', () => {
    expect(filterCampaigns(list, 'scheduled', '').map((c) => c.id)).toEqual([14])
    expect(filterCampaigns(list, 'done', '').map((c) => c.id)).toEqual([19])
  })

  it('finds text in the brief whatever its case, the Turkish way', () => {
    expect(filterCampaigns(list, '', 'İSTANBUL').map((c) => c.id)).toEqual([19])
    expect(filterCampaigns(list, '', 'istanbul').map((c) => c.id)).toEqual([19])
    expect(filterCampaigns(list, '', 'ISPARTA').map((c) => c.id)).toEqual([12])
    expect(filterCampaigns(list, '', 'indirim').map((c) => c.id)).toEqual([14])
  })

  it('combines both and can come up empty', () => {
    expect(filterCampaigns(list, 'done', 'indirim')).toEqual([])
    expect(filterCampaigns(list, 'scheduled', 'indirim').map((c) => c.id)).toEqual([14])
  })

  it('counts the campaigns in each state', () => {
    expect(countByState(list)).toEqual({ scheduled: 1, sending: 1, done: 1, cancelled: 1 })
    expect(countByState([])).toEqual({ scheduled: 0, sending: 0, done: 0, cancelled: 0 })
  })
})
