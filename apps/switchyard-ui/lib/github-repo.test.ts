import { githubRepoHref, stripGithubRemoteUrl } from "./github-repo";

describe("stripGithubRemoteUrl", () => {
  it("normalizes https and ssh remotes", () => {
    expect(stripGithubRemoteUrl("https://github.com/org/repo.git")).toBe("org/repo");
    expect(stripGithubRemoteUrl("git@github.com:org/repo.git")).toBe("org/repo");
  });
});

describe("githubRepoHref", () => {
  it("builds browse URLs from slugs", () => {
    expect(githubRepoHref("org/repo")).toBe("https://github.com/org/repo");
  });
});
