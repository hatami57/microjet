package dynamo

import "github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"

// SKCondition is an optional sort-key condition for QueryGSIPage.
// Construct one with the factory functions (SKEquals, SKBeginsWith, etc.)
// and pass nil to query by partition key only.
type SKCondition struct {
	apply func(expression.KeyConditionBuilder) expression.KeyConditionBuilder
}

func SKEquals(attr, val string) *SKCondition {
	return &SKCondition{func(k expression.KeyConditionBuilder) expression.KeyConditionBuilder {
		return k.And(expression.Key(attr).Equal(expression.Value(val)))
	}}
}

func SKBeginsWith(attr, prefix string) *SKCondition {
	return &SKCondition{func(k expression.KeyConditionBuilder) expression.KeyConditionBuilder {
		return k.And(expression.Key(attr).BeginsWith(prefix))
	}}
}

func SKBetween(attr, lo, hi string) *SKCondition {
	return &SKCondition{func(k expression.KeyConditionBuilder) expression.KeyConditionBuilder {
		return k.And(expression.Key(attr).Between(expression.Value(lo), expression.Value(hi)))
	}}
}

func SKLessThan(attr, val string) *SKCondition {
	return &SKCondition{func(k expression.KeyConditionBuilder) expression.KeyConditionBuilder {
		return k.And(expression.Key(attr).LessThan(expression.Value(val)))
	}}
}

func SKLessThanOrEqual(attr, val string) *SKCondition {
	return &SKCondition{func(k expression.KeyConditionBuilder) expression.KeyConditionBuilder {
		return k.And(expression.Key(attr).LessThanEqual(expression.Value(val)))
	}}
}

func SKGreaterThan(attr, val string) *SKCondition {
	return &SKCondition{func(k expression.KeyConditionBuilder) expression.KeyConditionBuilder {
		return k.And(expression.Key(attr).GreaterThan(expression.Value(val)))
	}}
}

func SKGreaterThanOrEqual(attr, val string) *SKCondition {
	return &SKCondition{func(k expression.KeyConditionBuilder) expression.KeyConditionBuilder {
		return k.And(expression.Key(attr).GreaterThanEqual(expression.Value(val)))
	}}
}
