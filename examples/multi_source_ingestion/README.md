# Multi-Source Fact Ingestion Example

This example demonstrates how Effectus can ingest facts from multiple heterogeneous sources while maintaining type safety and schema consistency.

## **Key Benefits**

### **Unified Type System**
- All facts become strongly-typed proto messages regardless of source
- Schema validation happens at ingestion time  
- Type safety propagates through entire pipeline

### **Dynamic Compilation**
- Rules compile against current schemas
- Hot-reload when schemas change
- Breaking change detection prevents runtime errors

### **Multi-Source Support**
- Kafka streams with schema registry
- HTTP webhooks with JSON transformation
- Database change data capture
- File watching with batch processing

## **Running the Example**

```bash
# Start dependencies
docker-compose up -d kafka postgres

# Run the dynamic system
go run main.go

# Send test facts
curl -X POST http://localhost:8081/webhook/external-events \
  -H "Content-Type: application/json" \
  -H "X-API-Key: test-token" \
  -d '{"user": {"id": "user123", "email_address": "test@example.com", "full_name": "Test User"}}'
```

This demonstrates how proto-first development enables Effectus to be a **universal fact ingestion platform** while maintaining mathematical rigor and type safety. 