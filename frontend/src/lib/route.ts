// Where the app is, kept in the URL hash so a reload or a shared link comes
// back to the same screen. Three places are enough; a router library is not.
//
//   (no hash)        the composer: brief -> audience -> content
//   #/kampanyalar    the campaign list
//   #/kampanya/12    the dashboard of campaign 12

export type Route = { name: 'compose' } | { name: 'campaigns' } | { name: 'campaign'; id: number }

export function parseHash(hash: string): Route {
  if (hash === '#/kampanyalar') return { name: 'campaigns' }
  const match = /^#\/kampanya\/([1-9]\d{0,14})$/.exec(hash)
  if (match) return { name: 'campaign', id: Number(match[1]) }
  return { name: 'compose' } // anything else, including a mistyped hash
}

export function formatHash(route: Route): string {
  switch (route.name) {
    case 'campaigns':
      return '#/kampanyalar'
    case 'campaign':
      return `#/kampanya/${route.id}`
    case 'compose':
      return ''
  }
}

export function sameRoute(a: Route, b: Route): boolean {
  return formatHash(a) === formatHash(b)
}
