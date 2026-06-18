import { useEffect, useRef } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchUser, getSettings } from "@/api";
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
    refetchInterval: (query) => (query.state.data ? false : 1500),
  });

  useEffect(() => {
    if (navigatedRef.current) {
      return;
    }

    if (data?.settings) {
      navigatedRef.current = true;
      const chatId = data.settings.LastHomeView === "chat" ? "new" : "launch";
      navigate({ to: "/c/$chatId", params: { chatId }, replace: true });
      return;
    }

    const timer = setTimeout(() => {
      if (!navigatedRef.current) {
        navigatedRef.current = true;
        navigate({
          to: "/c/$chatId",
          params: { chatId: "new" },
          replace: true,
        });
      }
    }, 5000);

    return () => clearTimeout(timer);
  }, [data, navigate]);

  // Pre-warm the user query so the chat route can render immediately.
  useEffect(() => {
    queryClient.prefetchQuery({
      queryKey: ["user"],
      queryFn: fetchUser,
    });
  }, [queryClient]);

  return <LoadingScreen message="Opening Ocelot..." />;
}
