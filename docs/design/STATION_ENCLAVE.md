# Station and enclave identity

Enclii combines the clarity of a railway station with the shelter and boundaries
of an enclave. Switchyard and Dispatch keep their existing names and navigation.

`StationIdentity` in `@enclii/ui-components/station-identity` supplies a static
isometric station and a two-dimensional track motif, drawn with actual ASCII in
monospace. The hero belongs at arrival surfaces such as sign-in; the compact mark
belongs in application headers. This is Enclii's own UI package, not a shared
ecosystem theme.

- Keep actions and permissions in ordinary, accessible controls. Decorative
  ASCII is `aria-hidden`; real headings carry the identity.
- Use semantic theme colors, short lines, and responsive type. The mark must fit
  a narrow mobile viewport and both light and dark themes.
- Decoration never claims service health, deployment progress, access level,
  capacity, or traffic. These require measured data and explicit unavailable states.
- Keep the foundation static. No animation or 3D runtime is needed for this
  isometric projection; reduced-motion users receive the same composition.

Boundary: this public design contract contains no operational topology or
identity records. Operational governance lives in
[internal-devops](https://github.com/madfam-org/internal-devops/blob/main/docs/repo-boundary-contract.md).
