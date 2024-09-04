# Contribute to kubernetes-node-specific-sizing

## Foreword

First off, thank you for taking the time to contribute!

It's worth mentioning that the purpose of this document is to provide you with a set of general guidelines and not
strict rules: nothing is set in stone and we welcome contributions about these contributing guidelines as well :-)

## TL;DR

We want to make contributing straightforward and easy for everyone. As such and unless otherwise stated, we will use the
traditional GitHub fork and pull workflow: any commit must be made to a feature/topic branch in a local fork and
submitted via a pull request before it can be merged. If you are familiar with GitHub (and Git), branching and opening a
pull request or an issue... then you should be able to start contributing right away.

It is **strongly advised** to contact the project owner(s) **before** working on implementing a new feature or making
any kind of large code refactoring.

Contributors must agree to the following:

- material without explicit copyright assignment will be assigned to [ManoMano](https://manomano.fr)
- apart from a few identified exceptions, material must be licensed under the repository's license

## Coding style

Follow the project's conventions and tooling.

Contact the contributors if you need help with tooling.

## Documentation

We consider documentation as important as code. Substantial contribution must always come with exhaustive documentation.

## Tests

Application and contributions should be tested and push for the highest quality standard.

## Git

Make sure you have a [GitHub account](https://github.com/join).
The *main* branch should be considered as the production/deploy branch.

### Workflow

Extensive information can be found in this excellent [forking workflow
tutorial](https://www.atlassian.com/git/tutorials/comparing-workflows#forking-workflow).

In a nutshell:

1. [Fork](https://help.github.com/articles/fork-a-repo) the repository and clone it locally.  

    ```bash
    git clone https://github.com/${USERNAME}/${REPONAME}
    cd ${REPONAME}
    ```

2. Create a topic branch where changes will be done.

    ```bash
    git checkout -b ${TOPIC_BRANCH}
    ```

3. Commit the changes in logical and incremental chunks and use [interactive
   rebase](https://help.github.com/articles/about-git-rebase) when needed.

   In your [commit message](http://tbaggery.com/2008/04/19/a-note-about-git-commit-messages.html), make sure to:

    - use the present tense
    - use the imperative mood
    - limit the first line to 72 characters
    - reference any associated issues and/or PRs (if applicable)

    ```bash
    git commit -am 'Add new feature...'
    ```

4. Push the topic branch to the remote forked repository.

    ```bash
    git push origin ${TOPIC_BRANCH}
    ```

5. Open a [Pull request](https://help.github.com/articles/about-pull-requests) to the upstream repository with a clear
   title and description.

6. Once the PR has been merged, the topic branch can be removed from the local fork.

    ```bash
    git branch -d ${TOPIC_BRANCH}
    git push origin --delete ${TOPIC_BRANCH}
    ```

### Syncing a fork with its upstream

This is used to keep a local fork up-to-date with the original upstream repository.

1. Connect the local to the original upstream repository.

    ```
    git remote add upstream https://github.com/${USERNAME}/${REPONAME}
    ```

2. Checkout, fetch and merge the upstream master branch to the local one.

    ```
    git checkout main
    git fetch upstream
    git merge upstream/master
    ```

3. Push changes to update to remote forked repository.

    ```
    git push
    ```

See [GitHub help](https://help.github.com/articles/syncing-a-fork) for more information.

## Issues

If you find a bug that you don't know how to fix, please create an [issue](https://guides.github.com/features/issues/):

- use a clear and descriptive title
- give a step by step explanation on how to reproduce the problem
- include as many details as possible, even ones that may seem irrelevant; [gists](https://help.github.com/articles/about-gists/) are a good way to include large amount of context and information
- describe what was already tried to fix the problem

> This document is adapted from [ManoMano Guidelines](https://github.com/ManoManoTech/ALaMano/blob/master/CONTRIBUTING.md)