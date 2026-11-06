# Rendered from the ticket's Acceptance (r73 R2, v18960-668).
# The step definitions are the lane's work: name the tests
# test_scenario_<slug> (the r54 naming contract).
Feature: v18960-686-newco-listing-verification-with-uiauto — 680c: verification (missing; done by hand in v1): after publish, a uiauto run opens the live lis
  Scenario: 1. a published listing
    Given a published listing
    When verification runs
    Then it reports PASS with the four fields matched or FAIL naming the mismatch, and the nightly ledger carries the row
  Scenario: 2. the listing page times out
    Given the listing page times out
    Then FAIL with the reason, never a silent pass. ESTIMATE 1.5 lane-days.
