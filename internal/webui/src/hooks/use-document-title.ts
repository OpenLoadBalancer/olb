import { useEffect } from "react"

const BASE_TITLE = "OpenLoadBalancer"

/**
 * useDocumentTitle sets the document title for the current page.
 * Pass a page name to get "Page Name | OpenLoadBalancer".
 * Call without arguments or with empty string to reset to base title.
 */
export function useDocumentTitle(title?: string) {
  useEffect(() => {
    const prev = document.title
    document.title = title ? `${title} | ${BASE_TITLE}` : BASE_TITLE
    return () => { document.title = prev }
  }, [title])
}
