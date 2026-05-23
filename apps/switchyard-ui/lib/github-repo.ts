/**
 * Normalize GitHub remote URLs to "owner/repo" slugs for display and links.
 */
export function stripGithubRemoteUrl(gitRepo: string | undefined | null): string {
  if (!gitRepo) return "";
  return gitRepo
    .replace(/^https?:\/\/github\.com\//, "")
    .replace(/^git@github\.com:/, "")
    .replace(/\.git$/, "")
    .replace(/\/$/, "");
}

export function githubRepoHref(gitRepo: string | undefined | null): string {
  const slug = stripGithubRemoteUrl(gitRepo);
  if (!slug) return "";
  if (gitRepo?.startsWith("http")) return gitRepo.replace(/\.git$/, "");
  return `https://github.com/${slug}`;
}
