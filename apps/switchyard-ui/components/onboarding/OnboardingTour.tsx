'use client';

import { useEffect, useRef } from 'react';
import { driver, type DriveStep, type Driver } from 'driver.js';
import 'driver.js/dist/driver.css';
import { useTour } from '@/contexts/TourContext';

// =============================================================================
// TOUR STEPS
// =============================================================================

const TOUR_STEPS: DriveStep[] = [
  {
    popover: {
      title: 'Welcome to the Foundry',
      description:
        "You're in. Let's get your first app live in three steps.",
      side: 'over',
      align: 'center',
    },
  },
  {
    element: '[data-tour="domains"]',
    popover: {
      title: 'Connect your Domain',
      description:
        'Link your Cloudflare domain for zero-trust ingress with automatic SSL certificates.',
      side: 'bottom',
      align: 'start',
    },
  },
  {
    element: '[data-tour="create-project"]',
    popover: {
      title: 'Deploy your first App',
      description:
        'Create a project, push your code, and watch it go live. Sovereign tier gives you up to 10 projects.',
      side: 'bottom',
      align: 'start',
    },
  },
];

// =============================================================================
// COMPONENT
// =============================================================================

export function OnboardingTour() {
  const { shouldShowTour, isTourActive, completeTour, setTourActive } = useTour();
  const driverRef = useRef<Driver | null>(null);

  useEffect(() => {
    // Auto-start tour for new paid users
    if (shouldShowTour && !isTourActive) {
      // Small delay to ensure DOM is ready
      const timer = setTimeout(() => {
        setTourActive(true);
      }, 500);
      return () => clearTimeout(timer);
    }
  }, [shouldShowTour, isTourActive, setTourActive]);

  useEffect(() => {
    if (!isTourActive) {
      // Cleanup if tour is deactivated
      if (driverRef.current) {
        driverRef.current.destroy();
        driverRef.current = null;
      }
      return;
    }

    // Initialize driver.js
    const driverObj = driver({
      showProgress: true,
      showButtons: ['next', 'previous', 'close'],
      steps: TOUR_STEPS,
      nextBtnText: 'Next',
      prevBtnText: 'Previous',
      doneBtnText: 'Get Started',
      progressText: '{{current}} of {{total}}',
      popoverClass: 'enclii-tour-popover',
      onDestroyStarted: () => {
        completeTour();
      },
      onCloseClick: () => {
        completeTour();
        driverObj.destroy();
      },
    });

    driverRef.current = driverObj;
    driverObj.drive();

    return () => {
      if (driverRef.current) {
        driverRef.current.destroy();
        driverRef.current = null;
      }
    };
  }, [isTourActive, completeTour]);

  // This component doesn't render anything visible
  // It just manages the driver.js instance
  return null;
}

export default OnboardingTour;
