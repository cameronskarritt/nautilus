import { Button } from "@workspace/ui/components/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card"

type SignInCardProps = {
  title: string
  description: string
  href: string
  pending: boolean
  available: boolean
  error?: string
  onRetry: () => void
}

export function SignInCard({
  title,
  description,
  href,
  pending,
  available,
  error,
  onRetry,
}: SignInCardProps) {
  return (
    <Card className="mx-auto w-full max-w-md">
      <CardHeader>
        <CardTitle className="text-2xl">{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        {available ? (
          <Button
            variant="outline"
            className="w-full"
            render={<a href={href} />}
            nativeButton={false}
          >
            Continue with Google
          </Button>
        ) : (
          <div className="space-y-3">
            <p role="status" className="text-sm text-muted-foreground">
              {pending
                ? "Loading sign-in options…"
                : "Google sign-in is not available right now."}
            </p>
            <Button
              variant="outline"
              className="w-full"
              disabled={pending}
              onClick={onRetry}
            >
              Try again
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
