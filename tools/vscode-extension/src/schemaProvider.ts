import * as vscode from 'vscode';
import * as fs from 'fs';
import * as path from 'path';
import * as yaml from 'yaml';

export interface EffectusSchema {
    name: string;
    type: 'fact' | 'verb';
    fields: { [key: string]: SchemaField };
    description?: string;
    version?: string;
}

export interface SchemaField {
    type: string;
    description?: string;
    required?: boolean;
    examples?: any[];
}

export class SchemaProvider {
    private schemas: Map<string, EffectusSchema> = new Map();
    private context: vscode.ExtensionContext;

    constructor(context: vscode.ExtensionContext) {
        this.context = context;
        this.loadSchemas();
    }

    async loadSchemas(): Promise<void> {
        const workspaceFolders = vscode.workspace.workspaceFolders;
        if (!workspaceFolders) {
            return;
        }

        for (const folder of workspaceFolders) {
            const schemaPath = path.join(folder.uri.fsPath, '.effectus', 'schemas');
            if (fs.existsSync(schemaPath)) {
                await this.loadSchemasFromDirectory(schemaPath);
            }
        }
    }

    private async loadSchemasFromDirectory(schemaPath: string): Promise<void> {
        try {
            const files = fs.readdirSync(schemaPath);
            
            for (const file of files) {
                if (file.endsWith('.yaml') || file.endsWith('.yml')) {
                    const filePath = path.join(schemaPath, file);
                    const content = fs.readFileSync(filePath, 'utf8');
                    const schema = yaml.parse(content) as EffectusSchema;
                    
                    if (schema && schema.name) {
                        this.schemas.set(schema.name, schema);
                    }
                }
            }
        } catch (error) {
            console.error('Failed to load schemas:', error);
        }
    }

    refreshSchemas(): void {
        this.schemas.clear();
        this.loadSchemas();
    }

    getSchema(name: string): EffectusSchema | undefined {
        return this.schemas.get(name);
    }

    getAllSchemas(): EffectusSchema[] {
        return Array.from(this.schemas.values());
    }

    provideCompletions(
        document: vscode.TextDocument, 
        position: vscode.Position
    ): vscode.CompletionItem[] {
        const lineText = document.lineAt(position).text;
        const wordRange = document.getWordRangeAtPosition(position);
        const currentWord = wordRange ? document.getText(wordRange) : '';

        const completions: vscode.CompletionItem[] = [];

        // Context-aware completions based on cursor position
        if (this.isInWhenBlock(document, position)) {
            // Provide fact field completions
            this.addFactCompletions(completions, lineText);
        } else if (this.isInThenBlock(document, position)) {
            // Provide verb completions
            this.addVerbCompletions(completions);
        }

        // Add keyword completions
        this.addKeywordCompletions(completions);

        return completions;
    }

    provideHover(
        document: vscode.TextDocument, 
        position: vscode.Position
    ): vscode.Hover | undefined {
        const wordRange = document.getWordRangeAtPosition(position);
        if (!wordRange) {
            return undefined;
        }

        const word = document.getText(wordRange);
        const schema = this.getSchema(word);

        if (schema) {
            const markdownString = new vscode.MarkdownString();
            markdownString.appendMarkdown(`**${schema.name}** (${schema.type})\n\n`);
            
            if (schema.description) {
                markdownString.appendMarkdown(`${schema.description}\n\n`);
            }

            markdownString.appendMarkdown('**Fields:**\n');
            for (const [fieldName, field] of Object.entries(schema.fields)) {
                markdownString.appendMarkdown(`- \`${fieldName}\`: ${field.type}`);
                if (field.description) {
                    markdownString.appendMarkdown(` - ${field.description}`);
                }
                markdownString.appendMarkdown('\n');
            }

            return new vscode.Hover(markdownString, wordRange);
        }

        return undefined;
    }

    provideDefinition(
        document: vscode.TextDocument, 
        position: vscode.Position
    ): vscode.Definition | undefined {
        const wordRange = document.getWordRangeAtPosition(position);
        if (!wordRange) {
            return undefined;
        }

        const word = document.getText(wordRange);
        const schema = this.getSchema(word);

        if (schema) {
            // Try to find the schema file
            const workspaceFolders = vscode.workspace.workspaceFolders;
            if (workspaceFolders) {
                for (const folder of workspaceFolders) {
                    const schemaPath = path.join(folder.uri.fsPath, '.effectus', 'schemas', `${word}.yaml`);
                    if (fs.existsSync(schemaPath)) {
                        return new vscode.Location(
                            vscode.Uri.file(schemaPath),
                            new vscode.Position(0, 0)
                        );
                    }
                }
            }
        }

        return undefined;
    }

    async generateDocumentation(): Promise<void> {
        const workspaceFolders = vscode.workspace.workspaceFolders;
        if (!workspaceFolders) {
            throw new Error('No workspace folder open');
        }

        const workspaceRoot = workspaceFolders[0].uri.fsPath;
        const docsPath = path.join(workspaceRoot, 'docs', 'schemas.md');

        let documentation = '# Effectus Schema Documentation\n\n';
        documentation += `Generated on: ${new Date().toISOString()}\n\n`;

        const factSchemas = this.getAllSchemas().filter(s => s.type === 'fact');
        const verbSchemas = this.getAllSchemas().filter(s => s.type === 'verb');

        if (factSchemas.length > 0) {
            documentation += '## Fact Schemas\n\n';
            for (const schema of factSchemas) {
                documentation += this.generateSchemaDoc(schema);
            }
        }

        if (verbSchemas.length > 0) {
            documentation += '## Verb Schemas\n\n';
            for (const schema of verbSchemas) {
                documentation += this.generateSchemaDoc(schema);
            }
        }

        // Ensure docs directory exists
        const docsDir = path.dirname(docsPath);
        if (!fs.existsSync(docsDir)) {
            fs.mkdirSync(docsDir, { recursive: true });
        }

        fs.writeFileSync(docsPath, documentation, 'utf8');
    }

    private generateSchemaDoc(schema: EffectusSchema): string {
        let doc = `### ${schema.name}\n\n`;
        
        if (schema.description) {
            doc += `${schema.description}\n\n`;
        }

        doc += '**Fields:**\n\n';
        doc += '| Field | Type | Required | Description |\n';
        doc += '|-------|------|----------|-------------|\n';

        for (const [fieldName, field] of Object.entries(schema.fields)) {
            const required = field.required ? '✓' : '';
            const description = field.description || '';
            doc += `| \`${fieldName}\` | ${field.type} | ${required} | ${description} |\n`;
        }

        doc += '\n';

        if (Object.values(schema.fields).some(f => f.examples)) {
            doc += '**Examples:**\n\n';
            doc += '```yaml\n';
            for (const [fieldName, field] of Object.entries(schema.fields)) {
                if (field.examples && field.examples.length > 0) {
                    doc += `${fieldName}: ${JSON.stringify(field.examples[0])}\n`;
                }
            }
            doc += '```\n\n';
        }

        return doc;
    }

    private isInWhenBlock(document: vscode.TextDocument, position: vscode.Position): boolean {
        // Simple heuristic: look backwards for "when {" without a closing "}"
        for (let i = position.line; i >= 0; i--) {
            const line = document.lineAt(i).text.trim();
            if (line.includes('when {')) {
                return true;
            }
            if (line.includes('then {')) {
                return false;
            }
        }
        return false;
    }

    private isInThenBlock(document: vscode.TextDocument, position: vscode.Position): boolean {
        // Simple heuristic: look backwards for "then {" without a closing "}"
        for (let i = position.line; i >= 0; i--) {
            const line = document.lineAt(i).text.trim();
            if (line.includes('then {')) {
                return true;
            }
            if (line.includes('}') && !line.includes('then {')) {
                return false;
            }
        }
        return false;
    }

    private addFactCompletions(completions: vscode.CompletionItem[], lineText: string): void {
        const factSchemas = this.getAllSchemas().filter(s => s.type === 'fact');
        
        for (const schema of factSchemas) {
            // Add schema name completion
            const schemaCompletion = new vscode.CompletionItem(
                schema.name, 
                vscode.CompletionItemKind.Class
            );
            schemaCompletion.detail = `Fact: ${schema.name}`;
            schemaCompletion.documentation = schema.description;
            completions.push(schemaCompletion);

            // Add field completions if we're accessing a fact
            if (lineText.includes(schema.name + '.')) {
                for (const [fieldName, field] of Object.entries(schema.fields)) {
                    const fieldCompletion = new vscode.CompletionItem(
                        fieldName,
                        vscode.CompletionItemKind.Field
                    );
                    fieldCompletion.detail = `${field.type}`;
                    fieldCompletion.documentation = field.description;
                    completions.push(fieldCompletion);
                }
            }
        }
    }

    private addVerbCompletions(completions: vscode.CompletionItem[]): void {
        const verbSchemas = this.getAllSchemas().filter(s => s.type === 'verb');
        
        for (const schema of verbSchemas) {
            const verbCompletion = new vscode.CompletionItem(
                schema.name,
                vscode.CompletionItemKind.Function
            );
            verbCompletion.detail = `Verb: ${schema.name}`;
            verbCompletion.documentation = schema.description;
            
            // Create snippet with required fields
            const requiredFields = Object.entries(schema.fields)
                .filter(([_, field]) => field.required)
                .map(([name, _], index) => `${name}: \${${index + 1}:value}`)
                .join('\n  ');
            
            if (requiredFields) {
                verbCompletion.insertText = new vscode.SnippetString(
                    `${schema.name} {\n  ${requiredFields}\n}`
                );
            }
            
            completions.push(verbCompletion);
        }
    }

    private addKeywordCompletions(completions: vscode.CompletionItem[]): void {
        const keywords = [
            'rule', 'when', 'then', 'emit', 'if', 'else', 'and', 'or', 'not',
            'in', 'exists', 'forall', 'within', 'since', 'after', 'before'
        ];

        for (const keyword of keywords) {
            const completion = new vscode.CompletionItem(
                keyword,
                vscode.CompletionItemKind.Keyword
            );
            completions.push(completion);
        }
    }
} 