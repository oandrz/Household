// Temporary stand-in so /sign-up/$token's route registration compiles and
// this task's tests run. Task 31 replaces this body entirely with the real
// screen: GET the preview (useSignUpPreview), collect household name,
// display name, currency and password, and POST .../complete
// (useCompleteSignUp).
//
// A separate module (not a component declared inline in router.tsx) because
// router.tsx already exports non-component values (routeTree, router) --
// eslint-plugin-react-refresh's only-export-components rule flags a file
// that also exports a component alongside those (see router.tsx's own
// comment on signInMagicRoute for the identical reason its route component
// is an inline, unexported function instead).
import { SignUpScreen } from "./SignUpScreen";

export function SignUpCompleteScreen({ token }: { token: string }) {
  return <SignUpScreen key={token} />;
}
