import type { Metadata } from "next";

/**
 * Privacy Policy — public legal page.
 *
 * Linked from the self-serve signup wizard (/signup) and listed as a public
 * route in middleware.ts. Static server component; keep it dependency-free.
 */

export const metadata: Metadata = {
  title: "Privacy Policy — Enclii",
  description:
    "Privacy Policy (Aviso de Privacidad) for the Enclii hosted platform, operated by Innovaciones MADFAM S.A.S. de C.V.",
};

const LAST_UPDATED = "2026-07-11";

export default function PrivacyPage() {
  return (
    <div className="bg-background flex min-h-screen flex-col">
      <header className="border-border w-full border-b">
        <div className="mx-auto flex max-w-3xl items-center justify-between px-4 py-4">
          <h1 className="text-enclii-blue text-2xl font-bold">Enclii</h1>
          <a href="/signup" className="text-muted-foreground hover:text-foreground text-sm">
            Back to signup
          </a>
        </div>
      </header>

      <main className="mx-auto w-full max-w-3xl flex-1 px-4 py-12">
        <h2 className="text-foreground text-3xl font-bold">Privacy Policy</h2>
        <p className="text-muted-foreground mt-2 text-sm">Last updated: {LAST_UPDATED}</p>

        <p className="text-muted-foreground mt-6 text-sm leading-relaxed">
          This Privacy Policy (Aviso de Privacidad) explains how{" "}
          <strong className="text-foreground">Innovaciones MADFAM S.A.S. de C.V.</strong>{" "}
          (&quot;MADFAM&quot;, &quot;we&quot;, &quot;us&quot;), with domicile in Cuernavaca,
          Morelos, Mexico, collects and processes personal data when you use the hosted
          Enclii platform at enclii.dev and app.enclii.dev (the &quot;Service&quot;). MADFAM
          is the data controller (responsable) for this processing under the Mexican Ley
          Federal de Protección de Datos Personales en Posesión de los Particulares
          (&quot;LFPDPPP&quot;).
        </p>

        <Section title="1. Data We Collect">
          <ul>
            <li>
              <strong className="text-foreground">Account data</strong> — email address and,
              optionally, company name, provided during signup or login.
            </li>
            <li>
              <strong className="text-foreground">Deployment metadata</strong> — project and
              service names, connected Git repository identifiers, build and deployment
              events, and configuration needed to run your workloads.
            </li>
            <li>
              <strong className="text-foreground">Usage metrics</strong> — resource
              consumption (CPU, memory, bandwidth, storage) and feature usage associated
              with your account.
            </li>
            <li>
              <strong className="text-foreground">Logs</strong> — platform and application
              logs, which may include IP addresses and request metadata, used for
              operations, security, and abuse prevention.
            </li>
            <li>
              <strong className="text-foreground">Billing status</strong>{" "}
              — subscription plan and payment status from Dhanam, MADFAM&apos;s billing
              service. We do not store your payment card details.
            </li>
            <li>
              <strong className="text-foreground">Support communications</strong> — messages
              you send us by email.
            </li>
          </ul>
        </Section>

        <Section title="2. Purposes of Processing">
          <p>We process personal data for the following purposes:</p>
          <ul>
            <li>providing, operating, and maintaining the Service (primary purpose);</li>
            <li>account management, authentication, and transactional email such as verification and billing notices (primary purpose);</li>
            <li>billing and collection for paid plans (primary purpose);</li>
            <li>security, fraud and abuse prevention, and platform integrity (primary purpose);</li>
            <li>complying with legal obligations (primary purpose); and</li>
            <li>improving the Service using aggregated or de-identified usage data (secondary purpose).</li>
          </ul>
        </Section>

        <Section title="3. Legal Basis">
          <p>
            We process personal data as necessary to perform our contract with you (the
            Terms of Service), to comply with legal obligations, on the basis of your
            consent where required by the LFPDPPP, and for legitimate purposes related to
            operating and securing the Service. By providing your data and using the
            Service, you consent to the processing described in this notice; where the law
            requires express consent, we will request it separately.
          </p>
        </Section>

        <Section title="4. Your Rights (ARCO)">
          <p>
            Under the LFPDPPP you have the rights of{" "}
            <strong className="text-foreground">
              Acceso, Rectificación, Cancelación, and Oposición
            </strong>{" "}
            (access, rectification, cancellation, and opposition), as well as the right to
            revoke your consent and to limit the use or disclosure of your personal data. To
            exercise these rights, email{" "}
            <a href="mailto:innovacionesmadfam@proton.me" className="underline">
              innovacionesmadfam@proton.me
            </a>{" "}
            from the address associated with your account, describing the right you wish to
            exercise. We will respond within the timeframes established by the LFPDPPP. You
            may also lodge a complaint with the Mexican data protection authority (INAI).
          </p>
        </Section>

        <Section title="5. Subprocessors and Transfers">
          <p>
            We use a small number of infrastructure providers to operate the Service. Data
            may be transferred to and processed by:
          </p>
          <ul>
            <li>
              <strong className="text-foreground">Hetzner Online GmbH</strong> —
              infrastructure hosting; servers located in the European Union.
            </li>
            <li>
              <strong className="text-foreground">Cloudflare, Inc.</strong> — CDN, DNS, and
              network security in front of the Service.
            </li>
            <li>
              <strong className="text-foreground">Resend</strong> — transactional email
              delivery (for example, signup verification).
            </li>
          </ul>
          <p>
            Billing is processed by Dhanam, MADFAM&apos;s own billing service. Transfers to
            the providers above are carried out under the conditions permitted by the
            LFPDPPP for processing on our behalf; these providers are bound by contractual
            obligations consistent with this notice.
          </p>
        </Section>

        <Section title="6. Data Retention">
          <ul>
            <li>Account and billing data are retained while your account is active and as required by law (for example, tax obligations).</li>
            <li>Logs and usage metrics are retained for limited operational windows and then deleted or aggregated.</li>
            <li>
              When you cancel your account or exercise cancellation rights, we delete or
              anonymize your personal data within a reasonable period, except where
              retention is required by law or for the defense of legal claims.
            </li>
          </ul>
        </Section>

        <Section title="7. Security">
          <p>
            We apply administrative, technical, and physical safeguards appropriate to the
            data we process, including encryption in transit (TLS), encrypted secret
            storage, access controls and least-privilege operations, tenant workload
            isolation, and security monitoring. No system is completely secure; we will
            notify affected users of security incidents as required by the LFPDPPP.
          </p>
        </Section>

        <Section title="8. Changes to This Policy">
          <p>
            We may update this Privacy Policy from time to time. We will post the updated
            version on this page and update the &quot;Last updated&quot; date, and for
            material changes we will provide additional notice (for example by email or
            in-product notice).
          </p>
        </Section>

        <Section title="9. Contact">
          <p>
            Innovaciones MADFAM S.A.S. de C.V. — Cuernavaca, Morelos, Mexico.
            <br />
            Privacy requests (including ARCO rights):{" "}
            <a href="mailto:innovacionesmadfam@proton.me" className="underline">
              innovacionesmadfam@proton.me
            </a>
          </p>
          <p>
            See also our <a href="/terms" className="underline">Terms of Service</a>.
          </p>
        </Section>
      </main>

      <footer className="border-border w-full border-t">
        <div className="text-muted-foreground mx-auto flex max-w-3xl items-center justify-between px-4 py-6 text-xs">
          <span>&copy; {new Date().getFullYear()} Innovaciones MADFAM S.A.S. de C.V.</span>
          <span className="space-x-4">
            <a href="/terms" className="hover:text-foreground">Terms</a>
            <a href="/privacy" className="hover:text-foreground">Privacy</a>
          </span>
        </div>
      </footer>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mt-10">
      <h3 className="text-foreground mb-3 text-xl font-semibold">{title}</h3>
      <div className="text-muted-foreground space-y-3 text-sm leading-relaxed [&_ul]:list-disc [&_ul]:space-y-1 [&_ul]:pl-6">
        {children}
      </div>
    </section>
  );
}
