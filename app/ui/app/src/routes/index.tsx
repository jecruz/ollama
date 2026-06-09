import { useEffect, useRef } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getSettings } from "@/api";
import { CURRENT_ONBOARDING_VERSION, homeChatId } from "@/lib/onboarding";
import LoadingScreen from "@/components/LoadingScreen";

export const Route = createFileRoute("/")({
  component: RouteComponent,
});

function RouteComponent() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const navigatedRef = useRef(false);

  const { data } = useQuery({
    queryKey: ["settings"],
    queryFn: getSettings,
    retry: false,
    refetchInterval: (query) => {
      // Once we have settings, stop polling.
      return query.state.data ? false : 1500;
    },
  });

  useEffect(() => {
    if (navigatedRef.current) return;
    // If we have settings, route based on onboarding status, then home view.
    if (data?.settings) {
      navigatedRef.current = true;
      if (data.settings.OnboardingVersion < CURRENT_ONBOARDING_VERSION) {
        navigate({ to: "/onboarding", replace: true });
        return;
      }
      const chatId = homeChatId();
      navigate({ to: "/c/$chatId", params: { chatId }, replace: true });
      return;
    }
    // Fallback: after 5s, if settings never loaded (e.g. token cookie
    // not being sent in this build), just go straight to the chat UI.
    const t = setTimeout(() => {
      if (!navigatedRef.current) {
        navigatedRef.current = true;
        navigate({ to: "/c/$chatId", params: { chatId: "new" }, replace: true });
      }
    }, 5000);
    return () => clearTimeout(t);
  }, [data, navigate]);

  // Pre-warm the user query so the chat route can render immediately.
  useEffect(() => {
    queryClient.prefetchQuery({
      queryKey: ["user"],
      queryFn: () => fetch("/api/me", { method: "POST", credentials: "include" }).then((r) => (r.ok ? r.json() : null)),
    });
  }, [queryClient]);

  return <LoadingScreen message="Opening Ollama..." />;
}
