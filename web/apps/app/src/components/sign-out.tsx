import { useMutation, useQueryClient } from "@tanstack/react-query"
import { logout } from "@workspace/api"
import { Button } from "@workspace/ui/components/button"

export function SignOut() {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: logout,
    onSuccess: async () => {
      await queryClient.cancelQueries()
      queryClient.clear()
      window.location.replace("/login")
    },
  })

  return (
    <div className="space-y-2">
      <Button
        variant="outline"
        disabled={mutation.isPending}
        onClick={() => mutation.mutate()}
      >
        {mutation.isPending ? "Signing out…" : "Sign out"}
      </Button>
      {mutation.isError && (
        <p role="alert" className="text-sm text-destructive">
          Sign-out failed. Please try again.
        </p>
      )}
    </div>
  )
}
