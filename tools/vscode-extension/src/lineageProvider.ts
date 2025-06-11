import * as vscode from 'vscode';

export interface SchemaNode {
    id: string;
    name: string;
    type: 'fact' | 'verb' | 'rule';
    description?: string;
    fields: string[];
}

export interface SchemaEdge {
    from: string;
    to: string;
    relationship: 'uses' | 'produces' | 'transforms';
    label?: string;
}

export interface LineageGraph {
    nodes: SchemaNode[];
    edges: SchemaEdge[];
}

export class LineageProvider {
    private context: vscode.ExtensionContext;
    private panel: vscode.WebviewPanel | undefined;

    constructor(context: vscode.ExtensionContext) {
        this.context = context;
    }

    async showLineageDiagram(): Promise<void> {
        const lineageData = await this.buildLineageGraph();
        
        if (this.panel) {
            this.panel.reveal();
        } else {
            this.panel = vscode.window.createWebviewPanel(
                'effectusLineage',
                'Effectus Schema Lineage',
                vscode.ViewColumn.Two,
                {
                    enableScripts: true,
                    retainContextWhenHidden: true,
                    localResourceRoots: [this.context.extensionUri]
                }
            );

            this.panel.onDidDispose(() => {
                this.panel = undefined;
            });
        }

        this.panel.webview.html = this.getWebviewContent(lineageData);
    }

    private async buildLineageGraph(): Promise<LineageGraph> {
        const nodes: SchemaNode[] = [];
        const edges: SchemaEdge[] = [];

        try {
            // Get all schemas from the workspace
            const schemas = await this.discoverSchemas();
            
            // Add schema nodes
            for (const schema of schemas) {
                nodes.push({
                    id: schema.name,
                    name: schema.name,
                    type: schema.type,
                    description: schema.description,
                    fields: Object.keys(schema.fields || {})
                });
            }

            // Get all rules from the workspace
            const rules = await this.discoverRules();
            
            // Add rule nodes and relationships
            for (const rule of rules) {
                nodes.push({
                    id: rule.name,
                    name: rule.name,
                    type: 'rule',
                    description: rule.description,
                    fields: []
                });

                // Add edges for facts used in when clauses
                for (const factUsed of rule.factsUsed) {
                    edges.push({
                        from: factUsed,
                        to: rule.name,
                        relationship: 'uses',
                        label: 'condition'
                    });
                }

                // Add edges for verbs emitted in then clauses
                for (const verbEmitted of rule.verbsEmitted) {
                    edges.push({
                        from: rule.name,
                        to: verbEmitted,
                        relationship: 'produces',
                        label: 'action'
                    });
                }
            }

        } catch (error) {
            console.error('Error building lineage graph:', error);
        }

        return { nodes, edges };
    }

    private async discoverSchemas(): Promise<any[]> {
        const schemas: any[] = [];
        const workspaceFolders = vscode.workspace.workspaceFolders;
        
        if (!workspaceFolders) {
            return schemas;
        }

        for (const folder of workspaceFolders) {
            const schemaPattern = new vscode.RelativePattern(
                folder, 
                '.effectus/schemas/**/*.{yaml,yml}'
            );
            
            const schemaFiles = await vscode.workspace.findFiles(schemaPattern);
            
            for (const file of schemaFiles) {
                try {
                    const document = await vscode.workspace.openTextDocument(file);
                    const content = document.getText();
                    const yaml = require('yaml');
                    const schema = yaml.parse(content);
                    
                    if (schema && schema.name) {
                        schemas.push(schema);
                    }
                } catch (error) {
                    console.error(`Error parsing schema file ${file.fsPath}:`, error);
                }
            }
        }

        return schemas;
    }

    private async discoverRules(): Promise<any[]> {
        const rules: any[] = [];
        const workspaceFolders = vscode.workspace.workspaceFolders;
        
        if (!workspaceFolders) {
            return rules;
        }

        for (const folder of workspaceFolders) {
            const rulePattern = new vscode.RelativePattern(folder, '**/*.{eff,effx}');
            const ruleFiles = await vscode.workspace.findFiles(rulePattern);
            
            for (const file of ruleFiles) {
                try {
                    const document = await vscode.workspace.openTextDocument(file);
                    const content = document.getText();
                    const rule = this.parseRuleFile(content, file.fsPath);
                    
                    if (rule) {
                        rules.push(rule);
                    }
                } catch (error) {
                    console.error(`Error parsing rule file ${file.fsPath}:`, error);
                }
            }
        }

        return rules;
    }

    private parseRuleFile(content: string, filePath: string): any | undefined {
        // Simple parser for extracting rule information
        // This is a basic implementation - a full parser would be more robust
        
        const lines = content.split('\n');
        let ruleName = '';
        const factsUsed: string[] = [];
        const verbsEmitted: string[] = [];
        let inWhenBlock = false;
        let inThenBlock = false;

        for (const line of lines) {
            const trimmed = line.trim();
            
            // Extract rule name
            const ruleMatch = trimmed.match(/rule\s+"([^"]+)"/);
            if (ruleMatch) {
                ruleName = ruleMatch[1];
                continue;
            }

            // Track block context
            if (trimmed.includes('when {')) {
                inWhenBlock = true;
                inThenBlock = false;
                continue;
            }
            if (trimmed.includes('then {')) {
                inThenBlock = true;
                inWhenBlock = false;
                continue;
            }
            if (trimmed === '}') {
                inWhenBlock = false;
                inThenBlock = false;
                continue;
            }

            // Extract facts used in when block
            if (inWhenBlock) {
                const factMatches = trimmed.match(/\b([a-zA-Z_][a-zA-Z0-9_]*)\./g);
                if (factMatches) {
                    for (const match of factMatches) {
                        const factName = match.slice(0, -1); // Remove the trailing dot
                        if (!factsUsed.includes(factName)) {
                            factsUsed.push(factName);
                        }
                    }
                }
            }

            // Extract verbs emitted in then block
            if (inThenBlock) {
                const emitMatch = trimmed.match(/emit\s+([a-zA-Z_][a-zA-Z0-9_]*)/);
                if (emitMatch) {
                    const verbName = emitMatch[1];
                    if (!verbsEmitted.includes(verbName)) {
                        verbsEmitted.push(verbName);
                    }
                }
            }
        }

        if (!ruleName) {
            // Generate rule name from file path if not found
            const fileName = filePath.split('/').pop() || 'unknown';
            ruleName = fileName.replace(/\.(eff|effx)$/, '');
        }

        return {
            name: ruleName,
            filePath,
            factsUsed,
            verbsEmitted,
            description: `Rule from ${filePath}`
        };
    }

    private getWebviewContent(lineageData: LineageGraph): string {
        return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Effectus Schema Lineage</title>
    <script src="https://unpkg.com/vis-network/standalone/umd/vis-network.min.js"></script>
    <style>
        body {
            font-family: var(--vscode-font-family);
            margin: 0;
            padding: 20px;
            background-color: var(--vscode-editor-background);
            color: var(--vscode-editor-foreground);
        }
        #lineage-container {
            width: 100%;
            height: 600px;
            border: 1px solid var(--vscode-panel-border);
            border-radius: 4px;
        }
        .info-panel {
            margin-top: 20px;
            padding: 15px;
            background-color: var(--vscode-textBlockQuote-background);
            border-radius: 4px;
            border-left: 4px solid var(--vscode-textLink-foreground);
        }
        .node-info {
            margin-bottom: 10px;
        }
        .node-type {
            display: inline-block;
            padding: 2px 8px;
            border-radius: 12px;
            font-size: 12px;
            font-weight: bold;
            text-transform: uppercase;
        }
        .node-type.fact { background-color: #4CAF50; color: white; }
        .node-type.verb { background-color: #2196F3; color: white; }
        .node-type.rule { background-color: #FF9800; color: white; }
    </style>
</head>
<body>
    <h1>Schema Lineage</h1>
    <p>This diagram shows the relationships between facts, verbs, and rules in your Effectus workspace.</p>
    
    <div id="lineage-container"></div>
    
    <div class="info-panel">
        <h3>Legend</h3>
        <div class="node-info">
            <span class="node-type fact">Fact</span> - Data structures available to rules
        </div>
        <div class="node-info">
            <span class="node-type verb">Verb</span> - Actions that can be executed
        </div>
        <div class="node-info">
            <span class="node-type rule">Rule</span> - Business logic that processes facts and emits verbs
        </div>
    </div>

    <script>
        const lineageData = ${JSON.stringify(lineageData)};
        
        // Transform data for vis.js
        const nodes = new vis.DataSet(
            lineageData.nodes.map(node => ({
                id: node.id,
                label: node.name,
                title: node.description || node.name,
                group: node.type,
                shape: node.type === 'rule' ? 'diamond' : 'box',
                color: {
                    background: node.type === 'fact' ? '#4CAF50' : 
                               node.type === 'verb' ? '#2196F3' : '#FF9800',
                    border: '#2B7CE9'
                }
            }))
        );
        
        const edges = new vis.DataSet(
            lineageData.edges.map(edge => ({
                from: edge.from,
                to: edge.to,
                label: edge.label || edge.relationship,
                arrows: 'to',
                color: {
                    color: edge.relationship === 'uses' ? '#4CAF50' : 
                           edge.relationship === 'produces' ? '#2196F3' : '#9E9E9E'
                }
            }))
        );
        
        const container = document.getElementById('lineage-container');
        const data = { nodes, edges };
        const options = {
            layout: {
                hierarchical: {
                    direction: 'UD',
                    sortMethod: 'directed',
                    nodeSpacing: 150,
                    levelSeparation: 200
                }
            },
            physics: {
                enabled: false
            },
            nodes: {
                font: {
                    color: '#FFFFFF',
                    size: 14
                },
                margin: 10
            },
            edges: {
                font: {
                    color: '#CCCCCC',
                    size: 12
                },
                smooth: {
                    type: 'cubicBezier',
                    forceDirection: 'vertical',
                    roundness: 0.4
                }
            }
        };
        
        const network = new vis.Network(container, data, options);
        
        // Handle node clicks
        network.on("click", function (params) {
            if (params.nodes.length > 0) {
                const nodeId = params.nodes[0];
                const node = lineageData.nodes.find(n => n.id === nodeId);
                if (node) {
                    const message = {
                        command: 'nodeClicked',
                        node: node
                    };
                    // Send message to VS Code extension
                    if (window.acquireVsCodeApi) {
                        const vscode = window.acquireVsCodeApi();
                        vscode.postMessage(message);
                    }
                }
            }
        });
    </script>
</body>
</html>`;
    }
} 