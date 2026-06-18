import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";
import { fetchUser } from "./api";
import { StreamingProvider } from "./contexts/StreamingContext";
import AppReady from "@/components/AppReady";

const queryClient = new QueryClient({
  defaultOptions: {
    mutations: {
      networkMode: "always", // Run mutations regardless of network state
    },
    queries: {
      networkMode: "always", // Allow queries even when offline (local server)
    },
  },
});

// Defer the initial fetch so it does not block the first paint. The router
// handles the case where user data is not loaded yet.
fetchUser()
  .then((userData) => {
    if (userData) {
      queryClient.setQueryData(["user"], userData);
    }
  })
  .catch(() => {
    // The user query will be retried by React Query where it is needed.
  });

const router = createRouter({
  routeTree,
  context: { queryClient },
});

// Register the router instance for type safety
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const rootElement = document.getElementById("root")!;
if (!rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement);
  root.render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <StreamingProvider>
          <AppReady />
          <RouterProvider router={router} />
        </StreamingProvider>
      </QueryClientProvider>
    </StrictMode>,
  );
}
