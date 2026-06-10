\## Understand Anything workflow



This project uses Understand Anything.



Before large changes, audits, refactors, or debugging across multiple files:



1\. Check whether `.understand-anything/knowledge-graph.json` exists.

2\. If the code has changed significantly since the last scan, run `/understand`.

3\. Use the Understand Anything graph first to identify relevant files and dependencies.

4\. Do not randomly read the whole repository.

5\. Use `/understand-diff` to inspect impact from current changes.

6\. Read only the files needed to verify the issue or make the fix.

7\. Before editing, explain the suspected issue, affected files, and risk.

