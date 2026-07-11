import type { Metadata } from "next";

/**
 * Terms of Service — public legal page.
 *
 * Linked from the self-serve signup wizard (/signup) and listed as a public
 * route in middleware.ts. Static server component; keep it dependency-free.
 */

export const metadata: Metadata = {
  title: "Terms of Service — Enclii",
  description:
    "Terms of Service for the Enclii hosted platform, operated by Innovaciones MADFAM S.A.S. de C.V.",
};

const LAST_UPDATED = "2026-07-11";

export default function TermsPage() {
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
        <h2 className="text-foreground text-3xl font-bold">Terms of Service</h2>
        <p className="text-muted-foreground mt-2 text-sm">Last updated: {LAST_UPDATED}</p>

        <p className="text-muted-foreground mt-6 text-sm leading-relaxed">
          These Terms of Service (the &quot;Terms&quot;) govern your access to and use of the
          hosted Enclii platform available at enclii.dev and app.enclii.dev (the
          &quot;Service&quot;), operated by <strong className="text-foreground">Innovaciones MADFAM
          S.A.S. de C.V.</strong>{" "}
          (&quot;MADFAM&quot;, &quot;we&quot;, &quot;us&quot;), a company incorporated in
          Cuernavaca, Morelos, Mexico. By creating an account or using the
          Service you agree to these Terms. If you use the Service on behalf of an
          organization, you represent that you have authority to bind that organization.
        </p>

        <Section title="1. The Service">
          <p>
            Enclii is a managed container deployment platform. It builds, deploys, runs, and
            scales containerized applications from your source code or container images, and
            provides related capabilities such as custom domains, TLS certificates, logs,
            metrics, and deployment automation. Features vary by plan and may evolve over
            time.
          </p>
        </Section>

        <Section title="2. Accounts and Responsibilities">
          <ul>
            <li>You must provide accurate account information and keep it up to date.</li>
            <li>
              You are responsible for safeguarding your credentials and API tokens, and for
              all activity that occurs under your account.
            </li>
            <li>
              You must notify us promptly at{" "}
              <a href="mailto:innovacionesmadfam@proton.me" className="underline">
                innovacionesmadfam@proton.me
              </a>{" "}
              if you suspect unauthorized use of your account.
            </li>
            <li>You must be at least 18 years old to use the Service.</li>
          </ul>
        </Section>

        <Section title="3. Acceptable Use">
          <p>You agree not to use the Service to:</p>
          <ul>
            <li>host, distribute, or transmit illegal content, malware, or phishing material;</li>
            <li>
              run cryptocurrency mining or similar workloads that abuse shared compute
              resources;
            </li>
            <li>
              exceed or circumvent the resource limits of your plan, or interfere with the
              stability of the platform or other tenants;
            </li>
            <li>probe, scan, or test the vulnerability of systems you do not own without authorization; or</li>
            <li>infringe the intellectual property or privacy rights of others.</li>
          </ul>
          <p>
            We may suspend workloads that violate this section, where practical after notice
            to you, or immediately when required to protect the platform or other customers.
          </p>
        </Section>

        <Section title="4. Plans and Billing">
          <ul>
            <li>
              Current plans and prices are listed on{" "}
              <a href="https://enclii.dev" className="underline">enclii.dev</a>: Community
              (free, self-hosted), Sovereign (paid, managed hosting), and Ecosystem (coming
              soon).
            </li>
            <li>
              Billing for paid plans is processed by Dhanam, MADFAM&apos;s billing service.
              Enclii does not store your payment card details.
            </li>
            <li>
              Paid subscriptions are billed in advance and renew monthly until cancelled.
              You can cancel at any time; cancellation takes effect at the end of the current
              billing period.
            </li>
            <li>Prices are exclusive of taxes. Applicable taxes, including IVA, are added where required by law.</li>
            <li>We will give reasonable advance notice of price changes, which apply from your next renewal.</li>
          </ul>
        </Section>

        <Section title="5. Open Source">
          <p>
            The Enclii software is open source under the AGPL-3.0 license, and the Community
            tier consists of self-hosting that software on your own infrastructure. Your use
            of the self-hosted software is governed by its license, not by these Terms. These
            Terms govern the hosted Service operated by MADFAM.
          </p>
        </Section>

        <Section title="6. Service Availability">
          <p>
            We operate the Service with reasonable efforts to keep it available and
            performant. No service level agreement (SLA) applies to the Community or
            Sovereign plans unless and until one is published. We may perform maintenance
            (announced on{" "}
            <a href="https://status.enclii.dev" className="underline">status.enclii.dev</a>{" "}
            where practical) and may modify or discontinue features with reasonable notice.
          </p>
        </Section>

        <Section title="7. Your Content and Intellectual Property">
          <ul>
            <li>
              You retain all rights to the code, container images, data, and content you
              deploy or store on the Service (&quot;Customer Content&quot;).
            </li>
            <li>
              You grant MADFAM a limited license to host, build, run, and transmit Customer
              Content solely to provide the Service.
            </li>
            <li>
              MADFAM and its licensors own the platform, its software, branding, and
              documentation. No rights are granted except as stated in these Terms or the
              applicable open-source licenses.
            </li>
            <li>You are responsible for ensuring you have the rights to deploy your Customer Content.</li>
          </ul>
        </Section>

        <Section title="8. Suspension and Termination">
          <ul>
            <li>You may stop using the Service and cancel your account at any time.</li>
            <li>
              We may suspend or terminate your access for material breach of these Terms,
              non-payment, legal requirement, or risk to the platform, with notice where
              practicable.
            </li>
            <li>
              Upon termination we will make reasonable efforts to allow you to export
              Customer Content for a limited period, after which it may be deleted in
              accordance with our{" "}
              <a href="/privacy" className="underline">Privacy Policy</a>.
            </li>
          </ul>
        </Section>

        <Section title="9. Disclaimer and Limitation of Liability">
          <p>
            The Service is provided &quot;as is&quot; and &quot;as available&quot;, without
            warranties of any kind, express or implied, to the maximum extent permitted by
            law. To the maximum extent permitted by law, MADFAM will not be liable for
            indirect, incidental, special, consequential, or punitive damages, or for loss of
            profits, revenue, or data, and our aggregate liability arising out of or relating
            to the Service is limited to the amounts you paid us for the Service in the
            twelve (12) months preceding the event giving rise to the claim. Nothing in these
            Terms excludes liability that cannot be excluded under applicable law.
          </p>
        </Section>

        <Section title="10. Changes to These Terms">
          <p>
            We may update these Terms from time to time. We will post the updated version on
            this page and update the &quot;Last updated&quot; date, and for material changes
            we will provide additional notice (for example by email or in-product notice).
            Continued use of the Service after changes take effect constitutes acceptance of
            the updated Terms.
          </p>
        </Section>

        <Section title="11. Governing Law">
          <p>
            These Terms are governed by the laws of Mexico. Any dispute arising from these
            Terms or the Service will be subject to the competent courts of Cuernavaca,
            Morelos, Mexico, and you waive any other jurisdiction to which you may be
            entitled by reason of your present or future domicile.
          </p>
        </Section>

        <Section title="12. Contact">
          <p>
            Innovaciones MADFAM S.A.S. de C.V. — Cuernavaca, Morelos, Mexico.
            <br />
            Questions about these Terms:{" "}
            <a href="mailto:innovacionesmadfam@proton.me" className="underline">
              innovacionesmadfam@proton.me
            </a>
          </p>
          <p>
            See also our <a href="/privacy" className="underline">Privacy Policy</a>.
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
