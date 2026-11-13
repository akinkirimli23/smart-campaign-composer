// Where the app is, kept in the URL hash so a reload or a shared link comes
// back to the same screen. Four places are enough; a router library is not.
//
//   (no hash)        the home page
//   #/olustur        the composer: brief -> audience -> content
//   #/kampanyalar    the campaign list
//   #/kampanya/12    the dashboard of campaign 12

export type Route = { name: 'home' } | { name: 'compose' } | { name: 'campaigns' } | { name: 'campaign'; id: number }

export function parseHash(hash: string): Route {
  if (hash === '#/olustur') return { name: 'compose' }
  if (hash === '#/kampanyalar') return { name: 'campaigns' }
  const match = /^#\/kampanya\/([1-9]\d{0,14})$/.exec(hash)
  if (match) return { name: 'campaign', id: Number(match[1]) }
  return { name: 'home' } // anything else, including a mistyped hash
}

export function formatHash(route: Route): string {
  switch (route.name) {
    case 'compose':
      return '#/olustur'
    case 'campaigns':
      return '#/kampanyalar'
    case 'campaign':
      return `#/kampanya/${route.id}`
    case 'home':
      return ''
  }
}

export function sameRoute(a: Route, b: Route): boolean {
  return formatHash(a) === formatHash(b)
}
