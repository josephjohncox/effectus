# Effectus Syntax Updates

This document outlines the updated syntax for Effectus rules based on recent changes to align implementation with documentation.

## Logical Operators in When Blocks

When blocks now support logical operators `&&` and `||` for combining conditions:

```
when {
  user.age > 18 && user.verified == true
}
```

You can also use parentheses for grouping expressions:

```
when {
  (user.role == "admin" || user.role == "manager") && user.active == true
}
```

## Multiple When-Then Blocks

Rules now support multiple when-then pairs within a single rule:

```
rule "user_actions" priority 10 {
  when {
    user.role == "admin"
  }
  then {
    allowAdminActions(user_id: user.id)
  }
  
  when {
    user.role == "customer" && user.status == "premium"
  }
  then {
    allowPremiumActions(user_id: user.id)
  }
}
```

## Named Parameters for Effects

Effects now use a structured parameter syntax with named parameters:

```
then {
  sendEmail(
    recipient: user.email,
    template: "welcome",
    subject: "Welcome to our platform"
  )
}
```

## Variable Binding

You can now bind the result of effects to variables using assignment syntax:

```
then {
  params = prepOptimizationParams(
    sales_history: store.sales_last_90_days,
    lead_times: store.supplier_lead_times
  )
  
  result = solveInventoryOptimization(
    method: "stochastic_programming",
    parameters: params
  )
  
  updateReorderPoints(
    store_id: store.id,
    new_values: result.reorder_points
  )
}
```

## Important Note on Effect Execution

Unlike some programming languages, Effectus does not use a `return` keyword. All effects in a `then` block are executed in sequence. The variable binding simply allows you to store and reference the results of previous operations.

```
// This is valid Effectus syntax
then {
  result = calculateValue(input: data.value)
  logResult(message: "Calculation complete", value: result)
}

// This is NOT valid Effectus syntax (no return keyword)
then {
  return calculateValue(input: data.value)  // INCORRECT
}
```

These syntax changes make Effectus rules more expressive and aligned with common programming patterns while maintaining the declarative nature of the rule language. 