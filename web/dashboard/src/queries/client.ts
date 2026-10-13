import { QueryClient } from '@tanstack/vue-query'
import { ApiError } from '@/apis/client'

// One explicit QueryClient instance, importable OUTSIDE component setup
// (pinia stores, plain modules). Components should still prefer
// useQueryClient(); this export exists for the few non-setup callers.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // One retry for transport hiccups; a 4xx answer (404 for a cleaned or
      // short id) is final — retrying only delays the not-found view, and a
      // hidden tab pauses the retry until it regains focus.
      retry: (count, err) => count < 1 && !(err instanceof ApiError && err.status >= 400 && err.status < 500),
      // Polling queries own their freshness; 2s staleTime just dedupes
      // multiple components mounting the same key in one navigation.
      staleTime: 2_000,
      refetchOnWindowFocus: true,
      // Background tab: stop polling (old usePolling checked document.hidden).
      refetchIntervalInBackground: false,
    },
  },
})
