---
name: testing-without-tautologies
description: Use when creating, editing, fixing, or reviewing tests; when adding mocks, assertions, smoke tests, unit tests, integration tests, e2e tests, or changing tests after failures.
---

# Testing Without Tautologies

## Core Idea

Tests should fail when the behavior they protect is broken. A passing test is only useful if it can reveal a meaningful problem.

Before writing or changing a test, ask: "What production change should make this test fail?" If the answer is unclear, redesign the test.

## Required Checks

Apply these checks to every new or modified test:

1. **Assert observable behavior**
   - Verify returned values, persisted state, rendered output, emitted events, API calls, errors, permissions, or other externally meaningful effects.
   - A test with no assertions is acceptable only when the failure mode is the behavior under test, such as "this constructor rejects invalid input." Prefer explicit assertions even then.

2. **Make mocks specific enough to detect regressions**
   - Mock expectations must verify important arguments, call counts, order, and branches when those details are part of the contract.
   - Avoid mocks that accept any input when the code must pass a particular value.

3. **Use distinct doubles for distinct branches**
   - Do not reuse one mock handler for success, error, incomplete, unauthorized, or other mutually exclusive paths.
   - Each branch should have its own spy/mock so the wrong branch cannot satisfy the expectation.

4. **Never mock the behavior under test**
   - Mock dependencies, boundaries, and slow or nondeterministic collaborators.
   - Do not replace the method, component, handler, query, reducer, or workflow whose behavior the test claims to verify.

5. **Investigate failures before changing expectations**
   - Do not flip expected values just to make a failing test pass.
   - First determine whether the production change is intended, then update the test to describe the new contract.

6. **Avoid mirror assertions**
   - Do not compute the expected value with the same production logic being tested.
   - Use literals, independently constructed fixtures, small hand-checked examples, or invariant/property assertions.

7. **Do not test upstream functionality**
   - Do not write tests whose real claim is that a trusted framework, SDK, standard library, database, parser, router, or generated client works as documented.
   - For example, do not test that Huma parses URL path parameters, query strings, status codes, or OpenAPI wiring correctly unless your code adds behavior around that parsing.
   - Test your contract at the boundary: that you registered the route you intend, pass the values you received into your domain code correctly, handle errors, and shape responses according to your API contract.
   - If an upstream behavior is surprising or previously regressed in your usage, write a narrow characterization test around your integration point and name the upstream assumption explicitly.

## Mutation Thought Experiment

Before finishing, mentally mutate the production code:

- Pass the wrong constant or argument.
- Call the wrong branch handler.
- Skip the state change.
- Return an empty or default value.
- Remove one important side effect.
- Replace an upstream library with a broken fake only where your code's boundary handling should notice.

At least one relevant test should fail for each realistic mutation. If none would fail, the test is probably tautological.

## Red Flags

- The test only proves that a mock was called, but not that the right dependency, argument, or branch was used.
- The setup and assertion both reuse the same object or helper in a way that guarantees equality.
- The test name promises behavior that is not asserted.
- The only possible failure is a panic, thrown exception, missing selector, or server crash.
- The test changed after a failure but the production behavior was not investigated.
- The test would still be meaningful if your application code were deleted and only the framework/library remained.
- The test asserts documented upstream mechanics, such as route parsing, JSON decoding, SQL placeholder behavior, or generated client serialization, without asserting your code's decision or contract.

## Practical Pattern

For each test, write a short intent sentence in your head:

> Given this setup, when the user/system does X, then Y observable behavior changes.

If you cannot fill in Y with something assertable, the test is not ready.
