import { ArrowRight } from 'lucide-react'

import { FooterLink } from './cards'

function Wordmark() {
  return (
    <div className="flex items-center gap-2">
      <div className="w-8 h-8 bg-solarpunk-deep rounded-lg flex items-center justify-center">
        <span className="text-white font-bold text-lg">E</span>
      </div>
      <span className="font-bold text-xl text-gray-900 dark:text-white">Enclii</span>
    </div>
  )
}

export function SiteNav() {
  return (
    <nav className="fixed top-0 left-0 right-0 z-50 bg-white/80 dark:bg-gray-900/80 backdrop-blur-md border-b border-gray-200 dark:border-gray-800">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between items-center h-16">
          <Wordmark />
          <div className="flex items-center gap-2 sm:gap-4">
            <a
              href="#buy-today"
              className="hidden sm:inline text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors text-sm"
            >
              Pricing
            </a>
            <a
              href="https://docs.enclii.dev"
              className="hidden sm:inline text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors text-sm"
            >
              Docs
            </a>
            <a
              href="https://github.com/madfam-org/enclii"
              className="hidden sm:inline text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white transition-colors text-sm"
            >
              GitHub
            </a>
            <a
              href="https://app.enclii.dev/signup"
              className="inline-flex items-center gap-2 bg-solarpunk-green text-solarpunk-slate px-3 py-2 sm:px-4 rounded-lg font-medium hover:bg-solarpunk-green-dim transition-colors text-sm sm:text-base"
            >
              Sign up
              <ArrowRight className="w-4 h-4" />
            </a>
          </div>
        </div>
      </div>
    </nav>
  )
}

export function SiteFooter() {
  return (
    <footer className="border-t border-gray-200 dark:border-gray-800 py-12 px-4 sm:px-6 lg:px-8">
      <div className="max-w-7xl mx-auto">
        <div className="grid md:grid-cols-4 gap-8">
          <div>
            <div className="mb-4">
              <Wordmark />
            </div>
            <p className="text-gray-600 dark:text-gray-400 text-sm">Deploy without the bill shock.</p>
          </div>
          <div>
            <h4 className="font-semibold text-gray-900 dark:text-white mb-4">Product</h4>
            <ul className="space-y-2 text-sm">
              <FooterLink href="https://app.enclii.dev">Dashboard</FooterLink>
              <FooterLink href="https://docs.enclii.dev">Documentation</FooterLink>
              <FooterLink href="https://docs.enclii.dev/changelog">Changelog</FooterLink>
            </ul>
          </div>
          <div>
            <h4 className="font-semibold text-gray-900 dark:text-white mb-4">Resources</h4>
            <ul className="space-y-2 text-sm">
              <FooterLink href="https://github.com/madfam-org/enclii">GitHub</FooterLink>
              <FooterLink href="https://docs.enclii.dev/guides">Guides</FooterLink>
              <FooterLink href="https://docs.enclii.dev/api">API Reference</FooterLink>
            </ul>
          </div>
          <div>
            <h4 className="font-semibold text-gray-900 dark:text-white mb-4">Company</h4>
            <ul className="space-y-2 text-sm">
              <FooterLink href="https://madfam.io">About</FooterLink>
              <FooterLink href="https://status.enclii.dev">Status</FooterLink>
            </ul>
          </div>
        </div>
        <div className="border-t border-gray-200 dark:border-gray-800 mt-12 pt-8 text-center text-gray-600 dark:text-gray-400 text-sm">
          <p>&copy; {new Date().getFullYear()} Madfam. All rights reserved.</p>
        </div>
      </div>
    </footer>
  )
}
