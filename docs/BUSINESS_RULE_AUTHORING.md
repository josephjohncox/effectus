# Business Rule Authoring

Effectus provides a streamlined workflow for business users to write rules using the native Effectus syntax with expr expressions.

## Rule Structure

```effectus
rule "rule_name" {
    when {
        // Conditional logic using expr syntax
        user.activity_score > 50 && 
        user.created_at.After(time.Now().Add(-7*24*time.Hour))
    }
    then {
        // Actions to execute
        send_email(
            to: user.email,
            template: "welcome",
            personalization: {name: user.profile.first_name}
        )
    }
}
```

## Language Features

### 1. No Imports Required
Facts and verbs are available directly in the rule context based on your workspace configuration.

### 2. Schema-Aware Autocompletion
The VS Code extension provides intelligent completions for:
- Fact field names and types
- Verb parameters and signatures
- Built-in functions and operators

### 3. Real-Time Validation
- Syntax checking as you type
- Type checking against schemas
- Performance analysis and optimization suggestions

## VS Code Extension

### Installation
```bash
# From the project root
cd tools/vscode-extension
npm install
vsce package
code --install-extension effectus-language-support-*.vsix
```

### Features
- **Syntax highlighting** for .eff/.effx files
- **Schema-aware IntelliSense** with autocompletion
- **Live error checking** and validation
- **Quick actions** for testing and documentation
- **Schema lineage visualization**

### Commands
- `Ctrl+Shift+P` → "Effectus: Validate Current Rule"
- `Ctrl+Shift+P` → "Effectus: Test Rule with Synthetic Data"
- `Ctrl+Shift+P` → "Effectus: Generate Schema Documentation"
- `Ctrl+Shift+P` → "Effectus: Show Schema Lineage"

## CLI Workflow

### Schema Management
```bash
# List available schemas
effectusc schema list

# Generate documentation
effectusc schema docs --output schema-docs/

# Validate schema compatibility
effectusc schema validate
```

### Rule Operations
```bash
# Validate rules
effectusc lint rules/*.eff

# Test with synthetic data
effectusc test rules/user_onboarding.eff --synthetic

# Compile and check performance
effectusc compile rules/ --optimize --report
```

### Development Server
```bash
# Start hot-reload development server
effectusc dev --watch rules/ --port 8080
```

## Schema Discovery

Facts and verbs are automatically discovered from your protobuf schemas:

```yaml
# .effectus/workspace.json
{
  "schema_sources": [
    "proto/acme/v1/facts/*.proto",
    "proto/acme/v1/verbs/*.proto"
  ],
  "fact_adapters": [
    "kafka://user-events",
    "http://localhost:3000/facts"
  ]
}
```

## Writing Effective Rules

### Conditional Logic
```effectus
rule "complex_conditions" {
    when {
        // Numeric comparisons
        user.activity_score > 75.0 &&
        
        // Date/time operations
        user.created_at.After(time.Now().Add(-30*24*time.Hour)) &&
        
        // String operations
        user.email contains "@company.com" &&
        
        // Array operations
        "premium" in user.tags &&
        
        // Null checks
        user.preferences != nil &&
        
        // Complex expressions
        sum(user.orders, {.amount}) > 1000.0
    }
    then {
        // Actions
    }
}
```

### Actions and Side Effects
```effectus
rule "multi_action_rule" {
    when {
        order.status == "completed"
    }
    then {
        // Email notification
        send_email(
            to: order.customer_email,
            template: "order_confirmation",
            personalization: {
                order_id: order.id,
                total: order.total_amount
            }
        )
        
        // Database update
        update_database(
            table: "analytics",
            operation: "increment",
            where: {customer_id: order.customer_id},
            data: {total_orders: 1, total_spent: order.total_amount}
        )
        
        // External API call  
        http_request(
            method: "POST",
            url: "https://api.fulfillment.com/orders",
            body: {
                order_id: order.id,
                items: order.items
            }
        )
    }
}
```

### Variables and Calculations
```effectus
rule "calculated_values" {
    when {
        user.activity_score > 60
    }
    then {
        // Local variables
        total_spent = sum(user.orders, {.amount})
        order_count = len(user.orders)
        avg_order = $total_spent / $order_count
        
        send_email(
            to: user.email,
            template: "spending_summary",
            personalization: {
                total_spent: $total_spent,
                order_count: $order_count,
                average_order: $avg_order
            }
        )
    }
}
```

## Testing and Validation

### Synthetic Data Testing
```bash
# Test specific rule with generated data
effectusc test rules/user_onboarding.eff --generate-facts=100

# Test with custom data file
effectusc test rules/promotion.eff --data test-data.json
```

### Performance Analysis
```bash
# Analyze rule performance
effectusc analyze rules/ --report performance.json

# Optimization suggestions
effectusc optimize rules/complex_rule.eff --suggest
```

## Team Collaboration

### Workspace Configuration
```json
{
  "name": "Marketing Automation",
  "version": "1.0.0",
  "schema_version": "v1",
  "team": {
    "owners": ["marketing-team"],
    "reviewers": ["engineering-team"]
  },
  "rules": {
    "require_review": true,
    "max_complexity": 10,
    "performance_budget": "100ms"
  }
}
```

### Code Review Integration
```bash
# Generate rule impact report for PR
effectusc review --base main --head feature/new-promotion-rules

# Schema compatibility check
effectusc schema diff --base v1.0.0 --head HEAD
```

## Best Practices

1. **Rule Naming**: Use descriptive, business-focused names
2. **Conditions**: Keep complex logic readable with proper formatting
3. **Performance**: Monitor rule execution times and optimize hot paths
4. **Testing**: Always test rules with realistic data before deployment
5. **Documentation**: Comment complex business logic for team understanding
6. **Modularity**: Break complex rules into smaller, focused rules when possible 