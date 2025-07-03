# GitHub Issue Migrator

This Go-based command-line tool is designed to migrate GitHub issues from one repository to another. It meticulously preserves essential details including labels, milestones, comments, and intra-issue links.

-----

## Prerequisites

Before you can use this tool, there are a few essential setup steps.

### 1\. Personal Access Token

First, you'll need a **GitHub Personal Access Token (PAT)** with the `repo` scope. This token is necessary for the tool to authenticate with the GitHub API and perform actions on your behalf.

Once you have your token, you must set it as an environment variable named `GITHUB_TOKEN`:

```bash
export GITHUB_TOKEN="your_personal_access_token"
```

### 2\. Exporting Issues

Next, you need to export the issues from your source repository using the official GitHub CLI (`gh`).

First, authenticate the `gh` CLI with your GitHub instance. If you are using a GitHub Enterprise server, you must include the `--hostname` flag.

```bash
gh auth login --hostname enterprise-github-domain
```

Be sure to replace `enterprise-github-domain` with your company's GitHub domain.

Once authenticated, run the following command to fetch the issues and save them to a file named `issues.json`:

```bash
gh issue list --state "open" --repo "SOURCE_OWNER/SOURCE_REPO" --json body,closed,closedAt,comments,createdAt,isPinned,labels,milestone,number,state,stateReason,title,updatedAt > issues.json
```

Remember to replace `"SOURCE_OWNER/SOURCE_REPO"` with the appropriate owner and repository name.

### 3\. (Optional) Modify the JSON File

After exporting, you can manually modify the content of the `issues.json` file. This is a powerful step for cleaning or altering data before it's imported.

**Important**: You should only change the *values* within the JSON file. **Do not modify the JSON structure** (i.e., do not remove or rename keys like "title", "body", "comments", etc.).

Examples of useful modifications include:

  * **Updating User IDs**: If a user's ID is different in the destination domain, you can perform a find-and-replace on the user's old ID to update it to the new one.
  * **Removing Sensitive Data**: You can review and delete any sensitive or unnecessary comments from the `comments` array in any issue.

-----

## Usage

With the prerequisites out of the way, you can now run the issue migrator. The tool requires three command-line flags to operate:

  * `--file`: The path to the `issues.json` file you created.
  * `--owner`: The owner of the **target** repository.
  * `--repo`: The name of the **target** repository.

Here's an example of how to execute the program:

```bash
go run main.go --file issues.json --owner "TARGET_OWNER" --repo "TARGET_REPO"
```

### 🧪 Important Recommendation

It is **highly recommended** that you first create a temporary test repository and run the import process against it. This allows you to verify that the migration works as expected and that all issues, comments, labels, and links are transferred correctly before running the tool on your final, production repository.

-----

## How It Works

The migration process is carried out in four distinct phases to ensure a smooth and accurate transfer of your issues.

### Phase 1: Data Collection

The tool begins by parsing the `issues.json` file to gather all unique labels and milestones from the source issues. This initial step ensures that all necessary metadata is identified before any changes are made to the target repository.

### Phase 2: Creating Labels and Milestones

Next, the migrator connects to your target repository. It checks for existing labels and milestones and creates any that are missing. This guarantees that when the issues are created, they can be correctly assigned their corresponding labels and milestones.

### Phase 3: Creating Issues and Comments

This is where the core migration happens. The tool iterates through each issue from your JSON file and creates a new corresponding issue in the target repository. All comments from the original issue are consolidated into a single, well-formatted comment in the new issue, with clear attribution to the original authors.

### Phase 4: Updating Issue Links

In the final phase, the tool intelligently updates the body of the newly created issues. It finds any references to other issues (e.g., `#42`) and updates them to point to the correct new issue numbers. This preserves the context and relationships between your migrated issues.
