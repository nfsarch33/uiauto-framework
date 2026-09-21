"""Laya decision-layer smoke for UI automation.

Proves the typed-decision role laya plays in this framework: given a page
state and UI-test questions, it returns structured decisions (choice +
calibrated probability) with no text generation. Run inside the container:

    python smoke.py
"""
import json
import sys

import laya

state = {
    "url": "https://shop.example.test/checkout",
    "title": "Checkout - Example Shop",
    "visible_text": (
        "Order summary 2 items total $84.00 "
        "Payment method [Payment failed - card declined] "
        "Retry payment  Change card  Contact support"
    ),
}

questions = {
    "next_action": {
        "type": "choice",
        "instructions": "Pick the next UI automation action for a checkout recovery test.",
        "criteria": {
            "retry_payment": "A retry payment control is visible and the failure is transient.",
            "change_card": "The failure suggests the card itself; switch payment method.",
            "escalate": "No recovery path is visible; hand the run to a human.",
            "assert_failure": "The test should record this state as the expected failure.",
        },
    },
    "error_visible": {
        "type": "noul",
        "instructions": "Does the page state show a payment error to the user?",
    },
}

agent = laya.load("convaiinnovations/laya")
result = agent.predict(state, questions)
print(json.dumps(result["answers"], indent=2, default=str))
if not result.get("answers"):
    sys.exit("no answers returned")
