'use client';

import * as React from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { ArrowLeft, Users, Building2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { useScope } from '@/contexts/ScopeContext';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/lib/utils';

// =============================================================================
// TYPES
// =============================================================================

interface FormData {
  name: string;
  slug: string;
  description: string;
  billing_email: string;
}

interface FormErrors {
  name?: string;
  slug?: string;
  billing_email?: string;
  submit?: string;
}

// =============================================================================
// COMPONENT
// =============================================================================

export default function CreateTeamPage() {
  const router = useRouter();
  const { createTeam } = useScope();
  const [isSubmitting, setIsSubmitting] = React.useState(false);
  const [formData, setFormData] = React.useState<FormData>({
    name: '',
    slug: '',
    description: '',
    billing_email: '',
  });
  const [errors, setErrors] = React.useState<FormErrors>({});
  const [autoSlug, setAutoSlug] = React.useState(true);

  // Generate slug from name
  const generateSlug = (name: string): string => {
    return name
      .toLowerCase()
      .trim()
      .replace(/[^\w\s-]/g, '')
      .replace(/[\s_-]+/g, '-')
      .replace(/^-+|-+$/g, '');
  };

  // Handle name change with auto-slug
  const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const name = e.target.value;
    setFormData((prev) => ({
      ...prev,
      name,
      slug: autoSlug ? generateSlug(name) : prev.slug,
    }));
    if (errors.name) {
      setErrors((prev) => ({ ...prev, name: undefined }));
    }
  };

  // Handle slug change manually
  const handleSlugChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const slug = e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '');
    setFormData((prev) => ({ ...prev, slug }));
    setAutoSlug(false);
    if (errors.slug) {
      setErrors((prev) => ({ ...prev, slug: undefined }));
    }
  };

  // Handle other field changes
  const handleChange = (
    e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>
  ) => {
    const { name, value } = e.target;
    setFormData((prev) => ({ ...prev, [name]: value }));
    if (errors[name as keyof FormErrors]) {
      setErrors((prev) => ({ ...prev, [name]: undefined }));
    }
  };

  // Validate form
  const validateForm = (): boolean => {
    const newErrors: FormErrors = {};

    if (!formData.name.trim()) {
      newErrors.name = 'Team name is required';
    } else if (formData.name.length < 2) {
      newErrors.name = 'Team name must be at least 2 characters';
    } else if (formData.name.length > 50) {
      newErrors.name = 'Team name must be 50 characters or less';
    }

    if (!formData.slug.trim()) {
      newErrors.slug = 'Slug is required';
    } else if (!/^[a-z0-9][a-z0-9-]*[a-z0-9]$/.test(formData.slug) && formData.slug.length > 1) {
      newErrors.slug = 'Slug must be lowercase alphanumeric with hyphens only';
    } else if (formData.slug.length === 1 && !/^[a-z0-9]$/.test(formData.slug)) {
      newErrors.slug = 'Slug must start with a letter or number';
    } else if (formData.slug.length < 2) {
      newErrors.slug = 'Slug must be at least 2 characters';
    }

    if (formData.billing_email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(formData.billing_email)) {
      newErrors.billing_email = 'Please enter a valid email address';
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  // Handle form submission
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!validateForm()) {
      return;
    }

    setIsSubmitting(true);
    setErrors({});

    try {
      await createTeam({
        name: formData.name.trim(),
        slug: formData.slug.trim(),
        description: formData.description.trim() || undefined,
        billing_email: formData.billing_email.trim() || undefined,
      });

      // Redirect to teams page on success
      router.push('/teams');
    } catch (err) {
      setErrors({
        submit: err instanceof Error ? err.message : 'Failed to create team',
      });
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="max-w-2xl mx-auto py-8 px-4 sm:px-6 lg:px-8">
      {/* Back link */}
      <Link
        href="/teams"
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground mb-6 transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to Teams
      </Link>

      {/* Header */}
      <div className="flex items-center gap-3 mb-8">
        <div className="h-12 w-12 rounded-lg bg-enclii-blue/10 flex items-center justify-center">
          <Users className="h-6 w-6 text-enclii-blue" />
        </div>
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Create a New Team</h1>
          <p className="text-muted-foreground">
            Teams help you collaborate with others on projects and services.
          </p>
        </div>
      </div>

      {/* Form Card */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Building2 className="h-5 w-5 text-muted-foreground" />
            Team Details
          </CardTitle>
          <CardDescription>
            Enter the information for your new team. You can update these settings later.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-6">
            {/* Error Alert */}
            {errors.submit && (
              <div className="bg-destructive/10 border border-destructive/20 text-destructive rounded-lg p-4">
                <p className="text-sm font-medium">{errors.submit}</p>
              </div>
            )}

            {/* Team Name */}
            <div className="space-y-2">
              <Label htmlFor="name">Team Name *</Label>
              <Input
                id="name"
                name="name"
                placeholder="My Awesome Team"
                value={formData.name}
                onChange={handleNameChange}
                className={cn(errors.name && 'border-destructive')}
                disabled={isSubmitting}
                autoFocus
              />
              {errors.name && (
                <p className="text-sm text-destructive">{errors.name}</p>
              )}
            </div>

            {/* Slug */}
            <div className="space-y-2">
              <Label htmlFor="slug">Slug *</Label>
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">enclii.dev/</span>
                <Input
                  id="slug"
                  name="slug"
                  placeholder="my-awesome-team"
                  value={formData.slug}
                  onChange={handleSlugChange}
                  className={cn('flex-1', errors.slug && 'border-destructive')}
                  disabled={isSubmitting}
                />
              </div>
              <p className="text-xs text-muted-foreground">
                URL-friendly identifier. Lowercase letters, numbers, and hyphens only.
              </p>
              {errors.slug && (
                <p className="text-sm text-destructive">{errors.slug}</p>
              )}
            </div>

            {/* Description */}
            <div className="space-y-2">
              <Label htmlFor="description">Description</Label>
              <Textarea
                id="description"
                name="description"
                placeholder="What does your team work on?"
                value={formData.description}
                onChange={handleChange}
                rows={3}
                disabled={isSubmitting}
              />
              <p className="text-xs text-muted-foreground">
                Optional. A brief description of your team.
              </p>
            </div>

            {/* Billing Email */}
            <div className="space-y-2">
              <Label htmlFor="billing_email">Billing Email</Label>
              <Input
                id="billing_email"
                name="billing_email"
                type="email"
                placeholder="billing@company.com"
                value={formData.billing_email}
                onChange={handleChange}
                className={cn(errors.billing_email && 'border-destructive')}
                disabled={isSubmitting}
              />
              <p className="text-xs text-muted-foreground">
                Optional. Email for billing notifications and invoices.
              </p>
              {errors.billing_email && (
                <p className="text-sm text-destructive">{errors.billing_email}</p>
              )}
            </div>

            {/* Actions */}
            <div className="flex items-center gap-4 pt-4">
              <Button
                type="submit"
                disabled={isSubmitting}
                className="flex-1 sm:flex-none"
              >
                {isSubmitting ? (
                  <>
                    <Spinner size="sm" className="mr-2" />
                    Creating...
                  </>
                ) : (
                  'Create Team'
                )}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => router.push('/teams')}
                disabled={isSubmitting}
              >
                Cancel
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      {/* Info Box */}
      <div className="mt-6 p-4 bg-muted/50 rounded-lg">
        <h3 className="text-sm font-medium mb-2">What happens next?</h3>
        <ul className="text-sm text-muted-foreground space-y-1">
          <li>• You&apos;ll be the owner of this team with full admin access</li>
          <li>• You can invite members and assign roles (admin, member, viewer)</li>
          <li>• Team projects and resources are shared with all members</li>
          <li>• Billing is managed at the team level for easier expense tracking</li>
        </ul>
      </div>
    </div>
  );
}
