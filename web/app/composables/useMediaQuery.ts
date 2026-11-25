/**
 * Reactive `matchMedia` result. The page is server-rendered, so the value starts as false and is read after mount;
 * the browser only ever sees the real value after hydration.
 */
export function useMediaQuery(query: string) {
  const matches = ref(false)
  let media: MediaQueryList | undefined
  const update = () => {
    matches.value = media?.matches ?? false
  }
  onMounted(() => {
    media = window.matchMedia(query)
    update()
    media.addEventListener('change', update)
  })
  onBeforeUnmount(() => media?.removeEventListener('change', update))
  return matches
}
